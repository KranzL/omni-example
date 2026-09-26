package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
)

func traceGroupKey(id string, repeat int) string {
	return fmt.Sprintf("%s#%d", id, repeat)
}

func TestRunnerCollectsPerCallTraces(t *testing.T) {
	q1 := resultFor("right", 0.01, 100)
	q1.Records = []llm.CallRecord{
		{Model: llm.ModelHaiku45, Tier: llm.TierCheap, Purpose: "agent-turn-1", CostUSD: 0.006, Usage: llm.Usage{InputTokens: 60, OutputTokens: 6}},
		{Model: llm.ModelHaiku45, Tier: llm.TierCheap, Purpose: "agent-turn-2", CostUSD: 0.004, Usage: llm.Usage{InputTokens: 40, OutputTokens: 4}},
	}
	qf := resultFor("a long story", 0.02, 200)
	qf.Records = []llm.CallRecord{
		{Model: llm.ModelHaiku45, Tier: llm.TierCheap, Purpose: "agent-turn-1", CostUSD: 0.02, Usage: llm.Usage{InputTokens: 200, OutputTokens: 20}},
	}
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {q1}, "QF": {qf}},
		calls:   map[string]int{},
	}
	rt := &stubRouter{
		tiers: map[string]llm.Tier{"Q1": llm.TierCheap, "QF": llm.TierMid},
		fails: map[string]int{"Q1": 1},
		calls: map[string]int{},
	}
	traces := NewTraceCollector()
	r := &Runner{
		Agent: fake,
		Judge: judgeStub{text: "CORRECT", cost: 0.0002},
		Questions: []Question{
			{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString},
			{ID: "f01", Difficulty: DifficultyHard, Text: "QF", AnswerType: TypeFreeText},
		},
		Expected:    map[string]any{"e01": "right", "f01": "the story"},
		Config:      router.NameAlwaysCheap,
		Router:      rt,
		Repeat:      2,
		Concurrency: 2,
		Traces:      traces,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	recs := traces.Ordered(len(lines))
	if len(recs) != 13 {
		t.Fatalf("trace records = %d, want 13", len(recs))
	}
	wantSizes := map[string]int{"e01#1": 4, "e01#2": 3, "f01#1": 3, "f01#2": 3}
	wantTier := map[string]llm.Tier{"e01": llm.TierCheap, "f01": llm.TierMid}
	groups := map[string][]TraceRecord{}
	for _, rec := range recs {
		groups[traceGroupKey(rec.QuestionID, rec.Repeat)] = append(groups[traceGroupKey(rec.QuestionID, rec.Repeat)], rec)
		if rec.AttemptTier != wantTier[rec.QuestionID] {
			t.Errorf("record %+v: attempt_tier = %s", rec, rec.AttemptTier)
		}
	}
	for key, want := range wantSizes {
		if len(groups[key]) != want {
			t.Errorf("group %s has %d records, want %d", key, len(groups[key]), want)
		}
	}
	var agentPurposes []string
	for _, rec := range groups["e01#1"] {
		if strings.HasPrefix(rec.Purpose, "agent-turn") {
			agentPurposes = append(agentPurposes, rec.Purpose)
		}
	}
	if strings.Join(agentPurposes, ",") != "agent-turn-1,agent-turn-2" {
		t.Errorf("e01#1 agent purposes = %v", agentPurposes)
	}
	judgeFound := 0
	for _, rec := range groups["f01#2"] {
		if rec.CostUSD == 0.0002 && rec.Model == llm.ModelHaiku45 {
			judgeFound++
		}
	}
	if judgeFound != 1 {
		t.Errorf("f01#2 judge records = %d, want 1", judgeFound)
	}
	for _, l := range lines {
		var sum float64
		for _, rec := range groups[traceGroupKey(l.ID, l.Repeat)] {
			sum += rec.CostUSD
		}
		if math.Abs(sum-(l.CostUSD+l.JudgeCostUSD)) > 1e-12 {
			t.Errorf("%s rep=%d: trace cost %v, line cost %v + judge %v", l.ID, l.Repeat, sum, l.CostUSD, l.JudgeCostUSD)
		}
	}
	path := filepath.Join(t.TempDir(), "traces.jsonl")
	if err := WriteTraceFile(path, recs); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"question_id":"e01"`, `"attempt_tier":"mid"`, `"purpose":"agent-turn-1"`, `"repeat":2`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("trace file lacks %s", want)
		}
	}
	var asCall llm.CallRecord
	dec := json.NewDecoder(strings.NewReader(strings.Split(string(raw), "\n")[0]))
	if err := dec.Decode(&asCall); err != nil {
		t.Fatalf("trace line does not decode as CallRecord: %v", err)
	}
}

func TestRunnerCollectsCascadeTraces(t *testing.T) {
	cheap := verified(attempt(llm.TierCheap, "wrong", 0.01, llm.ModelHaiku45, 0), router.VerdictReject, 0.001)
	cheap.Result.Records[0].Purpose = "agent-turn-1"
	mid := verified(attempt(llm.TierMid, "right", 0.02, llm.ModelSonnet5, 0), router.VerdictAccept, 0.001)
	mid.Result.Records[0].Purpose = "agent-turn-1"
	solver := stubSolver{
		outcomes: map[string]router.Outcome{"Q1": {Attempts: []router.Attempt{cheap, mid}}},
		errs:     map[string]error{},
	}
	traces := NewTraceCollector()
	r := &Runner{
		Cascade:     solver,
		Questions:   []Question{{ID: "h01", Difficulty: DifficultyHard, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"h01": "right"},
		Config:      "cascade-stub",
		Repeat:      1,
		Concurrency: 1,
		Traces:      traces,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	recs := traces.Ordered(len(lines))
	if len(recs) != 4 {
		t.Fatalf("trace records = %d, want 4", len(recs))
	}
	wantTiers := []llm.Tier{llm.TierCheap, llm.TierCheap, llm.TierMid, llm.TierMid}
	wantPurposes := []string{"agent-turn-1", "verifier", "agent-turn-1", "verifier"}
	wantCosts := []float64{0.01, 0.001, 0.02, 0.001}
	var sum float64
	for i, rec := range recs {
		if rec.QuestionID != "h01" || rec.Repeat != 1 {
			t.Errorf("record %d = %+v, want h01 rep 1", i, rec)
		}
		if rec.AttemptTier != wantTiers[i] || rec.Purpose != wantPurposes[i] || rec.CostUSD != wantCosts[i] {
			t.Errorf("record %d = tier %s purpose %s cost %v", i, rec.AttemptTier, rec.Purpose, rec.CostUSD)
		}
		sum += rec.CostUSD
	}
	if math.Abs(sum-lines[0].CostUSD) > 1e-12 {
		t.Errorf("trace cost %v, line cost %v", sum, lines[0].CostUSD)
	}
}

func TestTraceFilePath(t *testing.T) {
	cases := []struct {
		provider string
		config   string
		noCache  bool
		stamp    string
		want     string
	}{
		{"", "always-cheap", false, "s", filepath.Join("results", "traces", "always-cheap-s.jsonl")},
		{llm.ProviderAnthropic, "always-cheap", false, "s", filepath.Join("results", "traces", "always-cheap-s.jsonl")},
		{llm.ProviderVenice, "always-cheap", false, "s", filepath.Join("results", "traces", "venice-always-cheap-s.jsonl")},
		{"", "always-mid", true, "s", filepath.Join("results", "traces", "always-mid-nocache-s.jsonl")},
		{llm.ProviderVenice, "classifier", true, "s", filepath.Join("results", "traces", "venice-classifier-nocache-s.jsonl")},
	}
	for _, c := range cases {
		if got := TraceFilePath(c.provider, c.config, c.noCache, c.stamp); got != c.want {
			t.Errorf("TraceFilePath(%q, %q, %v) = %q, want %q", c.provider, c.config, c.noCache, got, c.want)
		}
	}
}
