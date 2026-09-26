package router

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/jev"
	"github.com/KranzL/omni-example/internal/llm"
)

var (
	_ Decider = (*JevClassifier)(nil)
	_ Decider = (*JevVerifier)(nil)
	_ Decider = (*Gated)(nil)
	_ Router  = (*DecisionRouter)(nil)
)

type fakeJev struct {
	answers   []map[string]jev.Answer
	err       error
	calls     int
	purpose   string
	state     any
	questions map[string]jev.Question
}

func (f *fakeJev) Ask(ctx context.Context, purpose string, state any, qs map[string]jev.Question, trace *llm.Trace) (jev.Response, llm.CallRecord, error) {
	i := f.calls
	f.calls++
	f.purpose, f.state, f.questions = purpose, state, qs
	rec := llm.CallRecord{Provider: jev.ProviderName, Model: "jev-1.13.0", Purpose: purpose, Usage: llm.Usage{InputTokens: 5000}, CostUSD: jev.Cost(jev.Usage{InputTokens: 5000}), LatencyMS: 300}
	if f.err != nil {
		rec.Error = f.err.Error()
		rec.CostUSD = 0
		trace.Append(rec)
		return jev.Response{}, rec, f.err
	}
	trace.Append(rec)
	return jev.Response{Model: "jev-1.13.0", Answers: f.answers[min(i, len(f.answers)-1)], Usage: jev.Usage{InputTokens: 5000}}, rec, nil
}

func choiceAnswer(label string, conf float64) map[string]jev.Answer {
	return map[string]jev.Answer{
		JevQuestionDifficulty: {Type: jev.TypeChoice, Choice: label, Confidence: conf, Probabilities: map[string]float64{label: conf}},
		JevQuestionVerdict:    {Type: jev.TypeChoice, Choice: label, Confidence: conf, Probabilities: map[string]float64{label: conf}},
		JevQuestionDepth:      {Type: jev.TypeScore, Score: 1.2, Confidence: 0.7},
	}
}

func classifierStub(t *testing.T, label string) *stubProvider {
	return &stubProvider{msg: toolMessage(t, ToolClassify, map[string]string{"label": label, "reason": "haiku says " + label}), cost: 0.0015}
}

func TestJevClassifierRequestShape(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelModerate, 0.93)}}
	ch, err := NewJevClassifier(f, "SEM").Decide(context.Background(), DecisionInput{Question: "Top 5 brands?"})
	if err != nil {
		t.Fatal(err)
	}
	if f.purpose != PurposeJevRoute {
		t.Errorf("purpose %q", f.purpose)
	}
	st := f.state.(map[string]any)
	if st["question"] != "Top 5 brands?" || st["semantic_layer"] != "SEM" || len(st) != 2 {
		t.Errorf("state %v", st)
	}
	d := f.questions[JevQuestionDifficulty]
	if d.Type != jev.TypeChoice {
		t.Errorf("difficulty question type %q", d.Type)
	}
	crit := d.Criteria.(map[string]any)
	for _, l := range DifficultyLabels() {
		if s, _ := crit[l].(string); s == "" {
			t.Errorf("criteria missing %s", l)
		}
	}
	if len(crit) != 3 || !strings.Contains(d.Instructions.(string), "`question`") {
		t.Errorf("difficulty question %+v", d)
	}
	if s := f.questions[JevQuestionDepth]; s.Type != jev.TypeScore || len(s.Criteria.([]any)) != 3 {
		t.Errorf("depth question %+v", s)
	}
	if ch.Label != LabelModerate || !ch.Scored || ch.Score != 0.93 || ch.Probs[LabelModerate] != 0.93 {
		t.Errorf("choice %+v", ch)
	}
	if !strings.Contains(ch.Reason, "depth 1.20 conf 0.70") || len(ch.CallRecords) != 1 || ch.Cost != 5000*0.042/1e6 {
		t.Errorf("choice %+v", ch)
	}
}

func TestJevVerifierRequestShape(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(VerdictReject, 0.85)}}
	ch, err := NewJevVerifier(f, "SEM").Decide(context.Background(), DecisionInput{Question: "Q", SQL: "SELECT 1", Rows: "1", Answer: "1"})
	if err != nil {
		t.Fatal(err)
	}
	st := f.state.(map[string]any)
	for k, want := range map[string]string{"question": "Q", "sql": "SELECT 1", "rows": "1", "answer": "1", "semantic_layer": "SEM"} {
		if st[k] != want {
			t.Errorf("state[%s] = %v, want %q", k, st[k], want)
		}
	}
	crit := f.questions[JevQuestionVerdict].Criteria.(map[string]any)
	if len(crit) != 2 || crit[VerdictAccept] == nil || crit[VerdictReject] == nil || f.purpose != PurposeJevVerify {
		t.Errorf("verdict question %+v", f.questions)
	}
	if ch.Label != VerdictReject || ch.Score != 0.85 {
		t.Errorf("choice %+v", ch)
	}
}

func TestJevDecideRejectsMissingOrWrongAnswer(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{{}}}
	if _, err := NewJevVerifier(f, "S").Decide(context.Background(), DecisionInput{}); err == nil {
		t.Error("missing answer must error")
	}
	f = &fakeJev{answers: []map[string]jev.Answer{{JevQuestionVerdict: {Type: jev.TypeScore}}}}
	if _, err := NewJevVerifier(f, "S").Decide(context.Background(), DecisionInput{}); err == nil {
		t.Error("score answer to a choice question must error")
	}
}

func TestJevRouterKeepsConfidentAnswer(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelEasy, 0.97)}}
	haiku := classifierStub(t, LabelHard)
	r := NewJevRouter(f, NewClassifier(haiku, "SEM"), "SEM", 0)
	d, err := r.Route(context.Background(), "How many users?")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name() != NameJevClassifier || d.Tier != llm.TierCheap || d.Label != LabelEasy {
		t.Errorf("decision %+v", d)
	}
	if haiku.req.Purpose != "" {
		t.Error("fallback must not be called above threshold")
	}
	if d.Gate == nil || d.Gate.FellBack || d.Gate.Threshold != JevDifficultyThresholds[LabelEasy] || len(d.CallRecords) != 1 {
		t.Errorf("gate %+v records %d", d.Gate, len(d.CallRecords))
	}
	if !strings.HasPrefix(d.Reason, "jev-difficulty easy score 0.97 >= 0.85") {
		t.Errorf("reason %q", d.Reason)
	}
}

func TestJevRouterFallsBelowThresholdAndChargesBoth(t *testing.T) {
	cases := []struct {
		name   string
		jev    *fakeJev
		reason string
	}{
		{"low score", &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelEasy, 0.84)}}, FallbackLowScore},
		{"per-label threshold", &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelModerate, 0.69)}}, FallbackLowScore},
		{"unknown label", &fakeJev{answers: []map[string]jev.Answer{choiceAnswer("trivial", 0.99)}}, FallbackLabel},
		{"jev error", &fakeJev{err: errors.New("status 529")}, FallbackError},
	}
	for _, tc := range cases {
		haiku := classifierStub(t, LabelHard)
		d, err := NewJevRouter(tc.jev, NewClassifier(haiku, "SEM"), "SEM", 0).Route(context.Background(), "Q")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if d.Label != LabelHard || d.Tier != llm.TierTop || haiku.req.Purpose != "route-classifier" {
			t.Errorf("%s: decision %+v", tc.name, d)
		}
		g := d.Gate
		if g == nil || !g.FellBack || g.FallbackReason != tc.reason || g.FallbackLabel != LabelHard || g.Fallback != NameClassifier {
			t.Errorf("%s: gate %+v", tc.name, g)
			continue
		}
		wantCost := g.PrimaryCost + 0.0015
		if math.Abs(d.Cost-wantCost) > 1e-12 || len(d.CallRecords) != 2 || d.CallRecords[0].Provider != jev.ProviderName {
			t.Errorf("%s: cost %v want %v, records %+v", tc.name, d.Cost, wantCost, d.CallRecords)
		}
		if tc.reason == FallbackError && (g.PrimaryError == "" || g.PrimaryCost != 0) {
			t.Errorf("%s: gate %+v", tc.name, g)
		}
		if !strings.Contains(d.Reason, "classifier: haiku says hard") {
			t.Errorf("%s: reason %q", tc.name, d.Reason)
		}
	}
}

func TestJevRouterFallbackErrorPropagates(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelEasy, 0.1)}}
	haiku := &stubProvider{err: errors.New("haiku down"), cost: 0.001}
	d, err := NewJevRouter(f, NewClassifier(haiku, "SEM"), "SEM", 0).Route(context.Background(), "Q")
	if err == nil || d.Gate == nil || !d.Gate.FellBack || d.Cost <= 0.001 {
		t.Errorf("decision %+v err %v", d, err)
	}
}

func TestGatedShadowSamplesFallback(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelEasy, 0.99)}}
	haiku := classifierStub(t, LabelModerate)
	var seen []string
	g := &Gated{
		Primary:          NewJevClassifier(f, "S"),
		Fallback:         NewClassifier(haiku, "S"),
		Thresholds:       JevDifficultyThresholds,
		DefaultThreshold: 1,
		ShadowRate:       0.5,
		Sample:           func() float64 { return 0.1 },
		OnShadow:         func(in DecisionInput, p, s Choice) { seen = append(seen, p.Label+">"+s.Label) },
	}
	ch, err := g.Decide(context.Background(), DecisionInput{Question: "Q"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Label != LabelEasy || !ch.Gate.Shadowed || ch.Gate.ShadowLabel != LabelModerate || ch.Gate.ShadowCost != 0.0015 {
		t.Errorf("choice %+v gate %+v", ch, ch.Gate)
	}
	if ch.Cost != ch.Gate.PrimaryCost || len(ch.CallRecords) != 1 {
		t.Errorf("shadow cost must not be charged to the decision: %+v", ch)
	}
	if len(seen) != 1 || seen[0] != "easy>moderate" {
		t.Errorf("OnShadow %v", seen)
	}
	g.Sample = func() float64 { return 0.9 }
	haiku.req = llm.Request{}
	ch, _ = g.Decide(context.Background(), DecisionInput{Question: "Q"})
	if ch.Gate.Shadowed || haiku.req.Purpose != "" {
		t.Error("sample above rate must not shadow")
	}
	if g.Point() != PointDifficulty || len(g.Labels()) != 3 || g.Name() != "jev-difficulty>classifier" {
		t.Errorf("gated identity %s %s %v", g.Name(), g.Point(), g.Labels())
	}
}

func TestCascadeWithJevVerifier(t *testing.T) {
	cases := []struct {
		name       string
		jevLabel   string
		jevConf    float64
		haiku      string
		wantTiers  []llm.Tier
		wantFell   bool
		wantHaikus int
	}{
		{"confident accept stops", VerdictAccept, 0.97, VerdictReject, []llm.Tier{llm.TierCheap}, false, 0},
		{"unsure accept asks haiku", VerdictAccept, 0.50, VerdictReject, []llm.Tier{llm.TierCheap, llm.TierMid}, true, 1},
		{"confident reject escalates", VerdictReject, 0.85, VerdictAccept, []llm.Tier{llm.TierCheap, llm.TierMid}, false, 0},
	}
	for _, tc := range cases {
		fake := &tierAgent{results: allGood()}
		f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(tc.jevLabel, tc.jevConf), choiceAnswer(VerdictAccept, 0.99)}}
		stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, tc.haiku), verdictMsg(t, VerdictAccept)}, cost: 0.002}
		v := NewJevGatedVerifier(f, NewVerifier(stub, llm.TierCheap, "SEM", nil), "SEM", 0)
		o, err := NewCascade(NameCascadeVerifyJev, fake, v).Solve(context.Background(), "q")
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !sameTiers(tiersOf(o), tc.wantTiers) {
			t.Errorf("%s: path %s", tc.name, o.Path())
		}
		first := o.Attempts[0]
		if first.VerifyGate == nil || first.VerifyGate.FellBack != tc.wantFell || first.VerifyGate.PrimaryLabel != tc.jevLabel {
			t.Errorf("%s: gate %+v", tc.name, first.VerifyGate)
		}
		if stub.calls != tc.wantHaikus {
			t.Errorf("%s: haiku verifier calls %d, want %d", tc.name, stub.calls, tc.wantHaikus)
		}
		st := f.state.(map[string]any)
		if st["rows"] == "" || !strings.Contains(st["rows"].(string), "count\n42") || st["answer"] != "42" {
			t.Errorf("%s: jev verifier state %v", tc.name, st)
		}
	}
}
