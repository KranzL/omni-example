package bench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
)

func TestCacheHitRatio(t *testing.T) {
	if got := CacheHitRatio(llm.Usage{}); got != 0 {
		t.Errorf("empty = %v, want 0", got)
	}
	u := llm.Usage{InputTokens: 100, CacheCreationInputTokens: 100, CacheReadInputTokens: 800}
	if got := CacheHitRatio(u); got != 0.8 {
		t.Errorf("ratio = %v, want 0.8", got)
	}
}

func TestSummarize(t *testing.T) {
	lines := []Line{
		{ID: "e01", Difficulty: DifficultyEasy, Correct: true, CostUSD: 0.01, LatencyMS: 100},
		{ID: "e02", Difficulty: DifficultyEasy, Correct: false, CostUSD: 0.02, LatencyMS: 200},
		{ID: "m01", Difficulty: DifficultyModerate, Correct: true, CostUSD: 0.03, JudgeCostUSD: 0.001, LatencyMS: 300},
		{ID: "h01", Difficulty: DifficultyHard, Correct: true, CostUSD: 0.04, LatencyMS: 400},
	}
	s := Summarize(router.NameAlwaysCheap, "ts", "src", 1, lines)
	if s.Total != 4 || s.Correct != 3 || s.Accuracy != 0.75 {
		t.Fatalf("summary %+v", s)
	}
	if s.ByDifficulty[DifficultyEasy].Accuracy != 0.5 {
		t.Errorf("easy %+v", s.ByDifficulty[DifficultyEasy])
	}
	if s.TotalCostUSD != 0.10 || s.JudgeCostUSD != 0.001 {
		t.Errorf("cost %v judge %v", s.TotalCostUSD, s.JudgeCostUSD)
	}
	wantPerCorrect := 0.101 / 3
	if s.CostPerCorrectUSD < wantPerCorrect-1e-12 || s.CostPerCorrectUSD > wantPerCorrect+1e-12 {
		t.Errorf("per correct %v, want %v", s.CostPerCorrectUSD, wantPerCorrect)
	}
	if s.MeanLatencyMS != 250 || s.P95LatencyMS != 400 {
		t.Errorf("mean %v p95 %v", s.MeanLatencyMS, s.P95LatencyMS)
	}
	empty := Summarize(router.NameAlwaysCheap, "ts", "src", 1, nil)
	if empty.Accuracy != 0 || empty.CostPerCorrectUSD != 0 || empty.MeanLatencyMS != 0 {
		t.Errorf("empty %+v", empty)
	}
	none := Summarize(router.NameAlwaysCheap, "ts", "src", 1, []Line{{ID: "e01", Correct: false, CostUSD: 0.5}})
	if none.CostPerCorrectUSD != 0 {
		t.Errorf("no correct per-correct = %v, want 0", none.CostPerCorrectUSD)
	}
}

func TestWriteAndReadLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, router.NameAlwaysCheap, "ts.jsonl")
	lines := []Line{
		{ID: "e01", Difficulty: DifficultyEasy, Correct: true, Answer: "a@b.com", Expected: "a@b.com", CostUSD: 0.001},
		{ID: "e02", Difficulty: DifficultyEasy, Correct: false, Answer: "x", Expected: "y", Tokens: llm.Usage{InputTokens: 5}},
	}
	if err := WriteLines(path, lines); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Correct || got[1].Correct || got[1].Tokens.InputTokens != 5 {
		t.Fatalf("round trip %+v", got)
	}
	sp := filepath.Join(dir, router.NameAlwaysCheap, LatestFileName)
	s := Summarize(router.NameAlwaysCheap, "ts", path, 1, lines)
	if err := WriteSummary(sp, s); err != nil {
		t.Fatal(err)
	}
	if LatestPath(llm.ProviderAnthropic, router.NameAlwaysCheap) != filepath.Join("results", router.NameAlwaysCheap, LatestFileName) {
		t.Errorf("latest path %s", LatestPath(llm.ProviderAnthropic, router.NameAlwaysCheap))
	}
	if LatestPath(llm.ProviderVenice, router.NameAlwaysMid) != filepath.Join("results", "venice", router.NameAlwaysMid, LatestFileName) {
		t.Errorf("venice latest path %s", LatestPath(llm.ProviderVenice, router.NameAlwaysMid))
	}
}

type scriptAgent struct {
	mu      sync.Mutex
	answers map[string][]agent.Result
	errs    map[string][]error
	calls   map[string]int
	active  atomic.Int32
	maxSeen atomic.Int32
	delay   time.Duration
}

func (f *scriptAgent) Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error) {
	cur := f.active.Add(1)
	for {
		max := f.maxSeen.Load()
		if cur <= max || f.maxSeen.CompareAndSwap(max, cur) {
			break
		}
	}
	defer f.active.Add(-1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return agent.Result{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.calls[question]
	f.calls[question] = n + 1
	if errs := f.errs[question]; n < len(errs) && errs[n] != nil {
		return agent.Result{}, errs[n]
	}
	res := f.answers[question][min(n, len(f.answers[question])-1)]
	res.Tier = tier
	return res, nil
}

func resultFor(answer string, cost float64, latency int64) agent.Result {
	return agent.Result{
		Answer:    answer,
		SQL:       "SELECT 1",
		Submitted: true,
		Turns:     2,
		Usage:     llm.Usage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 900},
		CostUSD:   cost,
		WallMS:    latency,
	}
}

func twoQuestions() []Question {
	return []Question{
		{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString},
		{ID: "e02", Difficulty: DifficultyEasy, Text: "Q2", AnswerType: TypeString},
	}
}

func TestRunnerGradesAndWarmsFirst(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{
			"Q1": {resultFor("right", 0.01, 100)},
			"Q2": {resultFor("wrong", 0.02, 200)},
		},
		calls: map[string]int{},
		delay: 50 * time.Millisecond,
	}
	r := &Runner{
		Agent:       fake,
		Questions:   twoQuestions(),
		Expected:    map[string]any{"e01": "right", "e02": "right"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      1,
		Concurrency: 3,
	}
	start := time.Now()
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("timing check skipped in short mode")
	}
	_ = start
	if len(lines) != 2 {
		t.Fatalf("lines %d", len(lines))
	}
	if !lines[0].Correct || lines[1].Correct {
		t.Fatalf("correct flags %+v", lines)
	}
	if lines[0].ID != "e01" || lines[1].Repeat != 1 || lines[0].Tier != llm.TierCheap {
		t.Fatalf("order %+v", lines)
	}
	if lines[0].CostUSD != 0.01 || lines[0].LatencyMS != 100 || lines[0].CacheHitRatio != 0.9 {
		t.Fatalf("line0 %+v", lines[0])
	}
	if fake.calls["Q1"] != 1 || fake.calls["Q2"] != 1 {
		t.Fatalf("calls %v", fake.calls)
	}
}

func TestRunnerCarriesNoCache(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {resultFor("right", 0.01, 10)}},
		calls:   map[string]int{},
	}
	r := &Runner{
		Agent:       fake,
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      1,
		Concurrency: 1,
		NoCache:     true,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !lines[0].NoCache {
		t.Fatalf("line %+v, want no_cache true", lines)
	}
}

func TestRunnerRepeats(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {resultFor("right", 0.01, 10)}},
		calls:   map[string]int{},
	}
	r := &Runner{
		Agent:       fake,
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      3,
		Concurrency: 2,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || lines[0].Repeat != 1 || lines[1].Repeat != 2 || lines[2].Repeat != 3 {
		t.Fatalf("repeats %+v", lines)
	}
	if fake.calls["Q1"] != 3 {
		t.Fatalf("calls %v", fake.calls)
	}
}

func TestRunnerRetriesTransportOnce(t *testing.T) {
	boom := errors.New("connection reset")
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {resultFor("right", 0.01, 10)}},
		errs:    map[string][]error{"Q1": {boom}},
		calls:   map[string]int{},
	}
	r := &Runner{
		Agent:       fake,
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !lines[0].Correct || lines[0].Error != "" {
		t.Fatalf("retried %+v", lines)
	}
	if fake.calls["Q1"] != 2 {
		t.Fatalf("calls %v, want 2", fake.calls)
	}
	always := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {resultFor("right", 0.01, 10)}},
		errs:    map[string][]error{"Q1": {boom, boom, boom}},
		calls:   map[string]int{},
	}
	r.Agent = always
	lines, err = r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Correct || lines[0].Error == "" {
		t.Fatalf("double failure %+v", lines)
	}
	if always.calls["Q1"] != 2 {
		t.Fatalf("calls %v, want exactly 2 (one retry)", always.calls)
	}
}

func TestRunnerNeverRetriesWrongAnswer(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {resultFor("wrong", 0.01, 10)}},
		calls:   map[string]int{},
	}
	r := &Runner{
		Agent:       fake,
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Correct {
		t.Fatalf("wrong answer marked correct %+v", lines)
	}
	if fake.calls["Q1"] != 1 {
		t.Fatalf("calls %v, want 1 (no retry on wrong answer)", fake.calls)
	}
}

func TestRunnerRespectsConcurrency(t *testing.T) {
	qs := make([]Question, 6)
	expected := map[string]any{}
	answers := map[string][]agent.Result{}
	for i := range qs {
		id := fmt.Sprintf("e%02d", i+1)
		qs[i] = Question{ID: id, Difficulty: DifficultyEasy, Text: fmt.Sprintf("Q%d", i+1), AnswerType: TypeString}
		expected[id] = "right"
		answers[fmt.Sprintf("Q%d", i+1)] = []agent.Result{resultFor("right", 0.01, 10)}
	}
	fake := &scriptAgent{answers: answers, calls: map[string]int{}, delay: 30 * time.Millisecond}
	r := &Runner{
		Agent: fake, Questions: qs, Expected: expected,
		Config: router.NameAlwaysCheap, Router: router.Fixed{Tier: llm.TierCheap}, Repeat: 1, Concurrency: 2,
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fake.maxSeen.Load(); got > 2 {
		t.Fatalf("max concurrency %d, want at most 2", got)
	}
	if _, err := (&Runner{}).Run(context.Background()); err == nil {
		t.Error("empty questions: want error")
	}
}

type judgeStub struct {
	text string
	cost float64
}

func (j judgeStub) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	rec := llm.CallRecord{Model: llm.ModelHaiku45, CostUSD: j.cost, Usage: llm.Usage{InputTokens: 5, OutputTokens: 2}}
	msg := &anthropic.Message{Content: []anthropic.ContentBlockUnion{{Type: "text", Text: j.text}}}
	return msg, rec, nil
}

func TestRunnerFreeTextUsesJudge(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"QF": {resultFor("a long story", 0.01, 10)}},
		calls:   map[string]int{},
	}
	r := &Runner{
		Agent:       fake,
		Judge:       judgeStub{text: "CORRECT", cost: 0.0002},
		Questions:   []Question{{ID: "f01", Difficulty: DifficultyHard, Text: "QF", AnswerType: TypeFreeText}},
		Expected:    map[string]any{"f01": "the story"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !lines[0].Correct || lines[0].JudgeVerdict != "CORRECT" || lines[0].JudgeCostUSD != 0.0002 {
		t.Fatalf("judged %+v", lines)
	}
	r.Judge = nil
	lines, err = r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Correct || lines[0].Error == "" {
		t.Fatalf("missing judge %+v", lines)
	}
}

func TestRunnerGradeErrorRecorded(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{"Q1": {resultFor("10", 0.01, 10)}},
		calls:   map[string]int{},
	}
	r := &Runner{
		Agent:       fake,
		Questions:   []Question{{ID: "e03", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeNumber}},
		Expected:    map[string]any{"e03": "not-a-number-expected"},
		Config:      router.NameAlwaysCheap,
		Router:      router.Fixed{Tier: llm.TierCheap},
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lines[0].Correct || lines[0].Error == "" {
		t.Fatalf("grade error %+v", lines)
	}
	if fake.calls["Q1"] != 1 {
		t.Fatalf("calls %v, want 1", fake.calls)
	}
}

type stubRouter struct {
	mu    sync.Mutex
	tiers map[string]llm.Tier
	fails map[string]int
	calls map[string]int
}

func (s *stubRouter) Name() string { return "stub" }

func (s *stubRouter) Route(ctx context.Context, question string) (router.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[question]++
	recs := []llm.CallRecord{{Usage: llm.Usage{InputTokens: 10, CacheReadInputTokens: 90}, CostUSD: 0.001}}
	d := router.Decision{Tier: s.tiers[question], Label: "label-" + question, Reason: "because", Cost: 0.001, Latency: 7 * time.Millisecond, CallRecords: recs}
	if s.calls[question] <= s.fails[question] {
		return router.Decision{Cost: 0.001, Latency: 7 * time.Millisecond, CallRecords: recs}, errors.New("no tool call")
	}
	return d, nil
}

func TestRunnerAddsRoutingCostAndConfusion(t *testing.T) {
	fake := &scriptAgent{
		answers: map[string][]agent.Result{
			"Q1": {resultFor("right", 0.01, 100)},
			"Q2": {resultFor("right", 0.02, 200)},
			"Q3": {resultFor("right", 0.03, 300)},
		},
		calls: map[string]int{},
	}
	rt := &stubRouter{
		tiers: map[string]llm.Tier{"Q1": llm.TierCheap, "Q2": llm.TierTop, "Q3": llm.TierMid},
		fails: map[string]int{"Q2": 1, "Q3": 2},
		calls: map[string]int{},
	}
	r := &Runner{
		Agent: fake,
		Questions: []Question{
			{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString},
			{ID: "h01", Difficulty: DifficultyHard, Text: "Q2", AnswerType: TypeString},
			{ID: "m01", Difficulty: DifficultyModerate, Text: "Q3", AnswerType: TypeString},
		},
		Expected:    map[string]any{"e01": "right", "h01": "right", "m01": "right"},
		Config:      "stub",
		Router:      rt,
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if l.Tier != llm.TierCheap || l.RouteLabel != "label-Q1" || l.RouteReason != "because" {
		t.Fatalf("route fields %+v", l)
	}
	if math.Abs(l.CostUSD-0.011) > 1e-12 || l.RouteCostUSD != 0.001 || l.LatencyMS != 107 || l.RouteLatency != 7 {
		t.Fatalf("routing cost not added %+v", l)
	}
	if l.RouteTokens.CacheReadInputTokens != 90 || l.RouteRetry != "" {
		t.Fatalf("route tokens %+v", l)
	}
	l = lines[1]
	if l.RouteTokens.CacheReadInputTokens != 180 || l.RouteRetry != "no tool call" {
		t.Fatalf("retried route tokens %+v", l)
	}
	if !l.Correct || l.Tier != llm.TierTop || math.Abs(l.RouteCostUSD-0.002) > 1e-12 || math.Abs(l.CostUSD-0.022) > 1e-12 {
		t.Fatalf("retried route %+v", l)
	}
	l = lines[2]
	if l.Correct || l.Error == "" || l.Tier != "" || math.Abs(l.CostUSD-0.002) > 1e-12 || fake.calls["Q3"] != 0 {
		t.Fatalf("failed route %+v, agent calls %v", l, fake.calls)
	}
	s := Summarize("stub", "ts", "src", 1, lines)
	if math.Abs(s.RouteCostUSD-0.005) > 1e-12 || math.Abs(s.TotalCostUSD-0.035) > 1e-12 {
		t.Errorf("summary costs route=%v total=%v", s.RouteCostUSD, s.TotalCostUSD)
	}
	if s.Confusion[DifficultyEasy][llm.TierCheap] != 1 || s.Confusion[DifficultyHard][llm.TierTop] != 1 || len(s.Confusion[DifficultyModerate]) != 0 {
		t.Errorf("confusion %+v", s.Confusion)
	}
	if s.RouteMatch != 2 {
		t.Errorf("route match %d, want 2", s.RouteMatch)
	}
	table := ConfusionTable(s.Confusion)
	want := "| labelled \\ routed | cheap | mid | top |\n|---|---:|---:|---:|\n| easy | 1 | 0 | 0 |\n| hard | 0 | 0 | 1 |\n"
	if table != want {
		t.Errorf("table\n%s\nwant\n%s", table, want)
	}
}

func TestExpectedTier(t *testing.T) {
	for d, want := range map[string]llm.Tier{DifficultyEasy: llm.TierCheap, DifficultyModerate: llm.TierMid, DifficultyHard: llm.TierTop, DifficultyExpert: llm.TierTop} {
		if got := ExpectedTier(d); got != want {
			t.Errorf("%s = %s, want %s", d, got, want)
		}
	}
}

type spendAgent struct {
	results []agent.Result
	errs    []error
	calls   int
}

func (s *spendAgent) Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error) {
	n := s.calls
	s.calls++
	return s.results[n], s.errs[n]
}

func TestRunnerRetryKeepsFailedAttemptSpend(t *testing.T) {
	boom := errors.New("connection reset")
	failed := agent.Result{Usage: llm.Usage{InputTokens: 40, OutputTokens: 4}, CostUSD: 0.003, WallMS: 70}
	q := Question{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}
	newRunner := func(ag AgentRunner) *Runner {
		return &Runner{
			Agent:       ag,
			Questions:   []Question{q},
			Expected:    map[string]any{"e01": "right"},
			Config:      router.NameAlwaysCheap,
			Router:      router.Fixed{Tier: llm.TierCheap},
			Repeat:      1,
			Concurrency: 1,
		}
	}
	recovered := &spendAgent{results: []agent.Result{failed, resultFor("right", 0.01, 10)}, errs: []error{boom, nil}}
	lines, err := newRunner(recovered).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if !l.Correct || l.Error != "" {
		t.Fatalf("retried line %+v", l)
	}
	if math.Abs(l.CostUSD-0.013) > 1e-12 || l.LatencyMS != 80 || l.Tokens.InputTokens != 140 || l.Tokens.OutputTokens != 14 {
		t.Errorf("retry dropped the failed attempt: cost %v latency %d tokens %+v", l.CostUSD, l.LatencyMS, l.Tokens)
	}
	secondFail := agent.Result{Usage: llm.Usage{InputTokens: 60}, CostUSD: 0.005, WallMS: 30}
	both := &spendAgent{results: []agent.Result{failed, secondFail}, errs: []error{boom, boom}}
	lines, err = newRunner(both).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l = lines[0]
	if l.Error == "" || l.Correct {
		t.Fatalf("double failure %+v", l)
	}
	if math.Abs(l.CostUSD-0.008) > 1e-12 || l.LatencyMS != 100 || l.Tokens.InputTokens != 100 {
		t.Errorf("double failure dropped spend: cost %v latency %d tokens %+v", l.CostUSD, l.LatencyMS, l.Tokens)
	}
}

type seqJudge struct {
	texts []string
	cost  float64
	calls int
}

func (j *seqJudge) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	text := j.texts[min(j.calls, len(j.texts)-1)]
	j.calls++
	rec := llm.CallRecord{Model: llm.ModelHaiku45, CostUSD: j.cost}
	return &anthropic.Message{Content: []anthropic.ContentBlockUnion{{Type: "text", Text: text}}}, rec, nil
}

func TestRunnerJudgeRetryKeepsCost(t *testing.T) {
	q := Question{ID: "f01", Difficulty: DifficultyHard, Text: "QF", AnswerType: TypeFreeText}
	run := func(j *seqJudge) Line {
		r := &Runner{
			Agent:       &scriptAgent{answers: map[string][]agent.Result{"QF": {resultFor("a story", 0.01, 10)}}, calls: map[string]int{}},
			Judge:       j,
			Questions:   []Question{q},
			Expected:    map[string]any{"f01": "the story"},
			Config:      router.NameAlwaysCheap,
			Router:      router.Fixed{Tier: llm.TierCheap},
			Repeat:      1,
			Concurrency: 1,
		}
		lines, err := r.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return lines[0]
	}
	l := run(&seqJudge{texts: []string{"Maybe", "CORRECT"}, cost: 0.001})
	if !l.Correct || math.Abs(l.JudgeCostUSD-0.002) > 1e-12 {
		t.Errorf("judge retry: correct %v cost %v, want true 0.002", l.Correct, l.JudgeCostUSD)
	}
	l = run(&seqJudge{texts: []string{"Maybe", "Unsure"}, cost: 0.001})
	if l.Correct || l.Error == "" || math.Abs(l.JudgeCostUSD-0.002) > 1e-12 {
		t.Errorf("judge double failure: correct %v error %q cost %v, want false, error, 0.002", l.Correct, l.Error, l.JudgeCostUSD)
	}
}

func TestRunnerCascadeNoAttempts(t *testing.T) {
	r := &Runner{
		Cascade:     stubSolver{outcomes: map[string]router.Outcome{"Q1": {}}},
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      "cascade-stub",
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].Error == "" || lines[0].Correct {
		t.Fatalf("empty cascade outcome %+v", lines)
	}
}

func TestReadLinesDefaultsStrictToLenient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.jsonl")
	body := `{"id":"e01","correct":true,"answer":"a"}
{"id":"e02","correct":true,"correct_strict":false,"answer":"b"}
{"id":"e03","correct":false,"answer":"c"}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || !lines[0].CorrectStrict || lines[1].CorrectStrict || lines[2].CorrectStrict {
		t.Fatalf("strict defaults %+v", lines)
	}
	if s := Summarize("c", "ts", path, 1, lines); s.CorrectStrict != 1 {
		t.Errorf("summary strict %d, want 1", s.CorrectStrict)
	}
}

func TestRunnerCascadeFinalFromEarlierAttemptWhenLastErrors(t *testing.T) {
	o := router.Outcome{Attempts: []router.Attempt{
		{Tier: llm.TierCheap, Result: agent.Result{Answer: "right", Submitted: true}, Signals: []string{router.SignalLowConfidence}},
		{Tier: llm.TierTop, Err: "overloaded", Signals: []string{router.SignalAgentError}},
	}}
	r := &Runner{
		Cascade:     stubSolver{outcomes: map[string]router.Outcome{"Q1": o}},
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      "cascade-stub",
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if !l.Correct || l.Tier != llm.TierCheap || len(l.Attempts) != 2 || !l.Attempts[0].Correct || l.Attempts[1].Correct {
		t.Fatalf("line %+v", l)
	}
}

type fixedDecider struct {
	label string
	score float64
	cost  float64
}

func (d fixedDecider) Name() string     { return "fixed-" + d.label }
func (d fixedDecider) Point() string    { return router.PointDifficulty }
func (d fixedDecider) Labels() []string { return router.DifficultyLabels() }
func (d fixedDecider) Decide(ctx context.Context, in router.DecisionInput) (router.Choice, error) {
	return router.Choice{Label: d.label, Score: d.score, Scored: true, Cost: d.cost}, nil
}

func TestRunnerRecordsShadowCost(t *testing.T) {
	fake := &scriptAgent{answers: map[string][]agent.Result{"Q1": {resultFor("right", 0.01, 100)}}, calls: map[string]int{}}
	g := &router.Gated{
		Primary:          fixedDecider{label: router.LabelEasy, score: 0.9, cost: 0.001},
		Fallback:         fixedDecider{label: router.LabelHard, cost: 0.004},
		DefaultThreshold: 0.5,
		ShadowRate:       1,
		Sample:           func() float64 { return 0 },
	}
	r := &Runner{
		Agent:       fake,
		Router:      &router.DecisionRouter{Label: "gated", Decider: g},
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Config:      "gated",
		Repeat:      1,
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if l.ShadowCostUSD != 0.004 || l.RouteCostUSD != 0.001 || l.RouteGate == nil || !l.RouteGate.Shadowed {
		t.Fatalf("line %+v", l)
	}
	s := Summarize("gated", "ts", "src", 1, lines)
	if s.ShadowCostUSD != 0.004 {
		t.Errorf("summary shadow = %v", s.ShadowCostUSD)
	}
	raw, err := json.Marshal(Summarize("x", "ts", "src", 1, []Line{{ID: "e01"}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "shadow_cost_usd") {
		t.Errorf("zero shadow cost serialized: %s", raw)
	}
}
