package bench

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
)

type stubSolver struct {
	outcomes map[string]router.Outcome
	errs     map[string]error
}

func (s stubSolver) Name() string { return "cascade-stub" }

func (s stubSolver) Solve(ctx context.Context, question string) (router.Outcome, error) {
	return s.outcomes[question], s.errs[question]
}

func attempt(tier llm.Tier, answer string, cost float64, model string, cacheWrite int64) router.Attempt {
	res := resultFor(answer, cost, 100)
	res.Tier = tier
	res.Usage.CacheCreationInputTokens = cacheWrite
	res.Records = []llm.CallRecord{{Model: model, Tier: tier, CostUSD: cost}}
	return router.Attempt{Tier: tier, Result: res}
}

func verified(a router.Attempt, verdict string, cost float64) router.Attempt {
	a.Verified = true
	a.Verdict = router.Verdict{Verdict: verdict, Reason: "r"}
	a.VerifyCost = cost
	a.VerifyLatency = 5 * time.Millisecond
	a.VerifyRecords = []llm.CallRecord{{Model: llm.ModelHaiku45, Purpose: "verifier", CostUSD: cost, Usage: llm.Usage{InputTokens: 50}}}
	if verdict == router.VerdictReject {
		a.Signals = []string{router.SignalVerifierReject}
	}
	return a
}

func TestRunnerCascadeChargesEveryAttempt(t *testing.T) {
	cheapWrongRejected := verified(attempt(llm.TierCheap, "wrong", 0.01, llm.ModelHaiku45, 7000), router.VerdictReject, 0.001)
	midRight := verified(attempt(llm.TierMid, "right", 0.02, llm.ModelSonnet5, 6000), router.VerdictAccept, 0.001)
	cheapWrongAccepted := verified(attempt(llm.TierCheap, "wrong", 0.01, llm.ModelHaiku45, 0), router.VerdictAccept, 0.001)
	cheapRightRejected := verified(attempt(llm.TierCheap, "right", 0.01, llm.ModelHaiku45, 0), router.VerdictReject, 0.001)
	lowConf := attempt(llm.TierMid, "wrong", 0.02, llm.ModelSonnet5, 0)
	lowConf.Signals = []string{router.SignalLowConfidence}
	top := attempt(llm.TierTop, "right", 0.04, llm.ModelOpus55, 5000)
	solver := stubSolver{
		outcomes: map[string]router.Outcome{
			"Q1": {Attempts: []router.Attempt{cheapWrongRejected, midRight}},
			"Q2": {Attempts: []router.Attempt{cheapWrongAccepted}},
			"Q3": {Attempts: []router.Attempt{cheapRightRejected, lowConf, top}},
			"Q4": {Attempts: []router.Attempt{{Tier: llm.TierCheap, Err: "boom"}}},
		},
		errs: map[string]error{"Q4": errors.New("boom")},
	}
	r := &Runner{
		Cascade: solver,
		Questions: []Question{
			{ID: "h01", Difficulty: DifficultyHard, Text: "Q1", AnswerType: TypeString},
			{ID: "e01", Difficulty: DifficultyEasy, Text: "Q2", AnswerType: TypeString},
			{ID: "x01", Difficulty: DifficultyExpert, Text: "Q3", AnswerType: TypeString},
			{ID: "e02", Difficulty: DifficultyEasy, Text: "Q4", AnswerType: TypeString},
		},
		Expected:    map[string]any{"h01": "right", "e01": "right", "x01": "right", "e02": "right"},
		Config:      "cascade-stub",
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if !l.Correct || l.Tier != llm.TierMid || l.Model != llm.ModelSonnet5 || l.RouteLabel != "cheap>mid" {
		t.Fatalf("line %+v", l)
	}
	if math.Abs(l.CostUSD-0.032) > 1e-12 || math.Abs(l.RouteCostUSD-0.002) > 1e-12 {
		t.Errorf("cost %v route %v, want 0.032 and 0.002", l.CostUSD, l.RouteCostUSD)
	}
	if l.Tokens.CacheCreationInputTokens != 13000 || l.RouteTokens.InputTokens != 100 {
		t.Errorf("tokens %+v route %+v", l.Tokens, l.RouteTokens)
	}
	if len(l.Attempts) != 2 || l.Attempts[0].Correct || !l.Attempts[1].Correct {
		t.Fatalf("attempts %+v", l.Attempts)
	}
	a0, a1 := l.Attempts[0], l.Attempts[1]
	if a0.Model != llm.ModelHaiku45 || a0.Tokens.CacheCreationInputTokens != 7000 || a1.Model != llm.ModelSonnet5 || a1.Tokens.CacheCreationInputTokens != 6000 {
		t.Errorf("each attempt must show its own model and cache write: %+v %+v", a0, a1)
	}
	if a0.Verdict != router.VerdictReject || a0.Signals[0] != router.SignalVerifierReject || a0.VerifyCostUSD != 0.001 {
		t.Errorf("attempt 0 %+v", a0)
	}
	if l.LatencyMS != 210 || l.RouteLatency != 10 {
		t.Errorf("latency %d route %d", l.LatencyMS, l.RouteLatency)
	}
	if lines[1].Correct || lines[1].Tier != llm.TierCheap || len(lines[1].Attempts) != 1 {
		t.Errorf("line 2 %+v", lines[1])
	}
	if lines[3].Error == "" || lines[3].Correct {
		t.Errorf("errored cascade %+v", lines[3])
	}
	s := Summarize("cascade-stub", "ts", "src", 1, lines)
	if e := s.Escalation[DifficultyEasy]; e.Total != 2 || e.Escalated != 0 {
		t.Errorf("easy escalation %+v", e)
	}
	if e := s.Escalation[DifficultyExpert]; e.Escalated != 1 || e.ReachedTop != 1 || e.Rate != 1 {
		t.Errorf("expert escalation %+v", e)
	}
	if s.Signals[router.SignalVerifierReject] != 2 || s.Signals[router.SignalLowConfidence] != 1 {
		t.Errorf("signals %+v", s.Signals)
	}
	v := s.Verifier
	if v == nil || v.Verified != 4 || v.AcceptedCorrect != 1 || v.AcceptedWrong != 1 || v.RejectedCorrect != 1 || v.RejectedWrong != 1 {
		t.Fatalf("verifier %+v", v)
	}
	if v.FalseRejectRate != 0.5 || v.FalseAcceptRate != 0.5 {
		t.Errorf("rates %+v", v)
	}
	if s.Confusion[DifficultyHard][llm.TierMid] != 1 || s.Confusion[DifficultyExpert][llm.TierTop] != 1 {
		t.Errorf("escalation matrix %+v", s.Confusion)
	}
	report := CascadeReport(s)
	for _, want := range []string{"| expert | 1 | 1 | 1.00 | 1 |", "verifier_reject=2", "false_reject_rate=0.500"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
	if CascadeReport(Summarize("x", "ts", "src", 1, []Line{{ID: "e01"}})) != "" {
		t.Error("non-cascade summary must have no cascade report")
	}
}

func TestRunnerCascadeWithRealCascade(t *testing.T) {
	low := resultFor("wrong", 0.01, 100)
	low.Confidence = "low"
	right := resultFor("right", 0.02, 100)
	right.Confidence = "high"
	fake := &scriptAgent{answers: map[string][]agent.Result{"Q1": {low, right}}, calls: map[string]int{}}
	r := &Runner{
		Cascade:     router.NewCascade(router.NameCascadeSignals, fake, nil),
		Questions:   []Question{{ID: "e01", Difficulty: DifficultyEasy, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"e01": "right"},
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	l := lines[0]
	if !l.Correct || l.Tier != llm.TierMid || math.Abs(l.CostUSD-0.03) > 1e-12 || l.RouteReason != "cheap: low_confidence; mid: final" {
		t.Errorf("line %+v", l)
	}
}

func TestRunnerCascadeRecordsShadowCost(t *testing.T) {
	cheap := verified(attempt(llm.TierCheap, "wrong", 0.01, llm.ModelHaiku45, 0), router.VerdictReject, 0.001)
	cheap.VerifyShadow = 0.003
	mid := verified(attempt(llm.TierMid, "right", 0.02, llm.ModelSonnet5, 0), router.VerdictAccept, 0.001)
	mid.VerifyShadow = 0.002
	r := &Runner{
		Cascade:     stubSolver{outcomes: map[string]router.Outcome{"Q1": {Attempts: []router.Attempt{cheap, mid}}}},
		Questions:   []Question{{ID: "h01", Difficulty: DifficultyHard, Text: "Q1", AnswerType: TypeString}},
		Expected:    map[string]any{"h01": "right"},
		Concurrency: 1,
	}
	lines, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(lines[0].ShadowCostUSD-0.005) > 1e-12 || math.Abs(lines[0].RouteCostUSD-0.002) > 1e-12 {
		t.Fatalf("line %+v", lines[0])
	}
}
