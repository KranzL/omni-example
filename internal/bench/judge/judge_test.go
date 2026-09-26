package judge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/llm"
)

type stubProvider struct {
	msg  *anthropic.Message
	err  error
	req  llm.Request
	cost float64
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	s.req = req
	rec := llm.CallRecord{Model: llm.ModelHaiku45, Tier: req.Tier, Purpose: req.Purpose, CostUSD: s.cost, LatencyMS: 5}
	trace.Append(rec)
	return s.msg, rec, s.err
}

type seqProvider struct {
	msgs  []*anthropic.Message
	reqs  []llm.Request
	cost  float64
	calls int
}

func (s *seqProvider) Name() string { return "seq" }

func (s *seqProvider) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	i := s.calls
	s.calls++
	s.reqs = append(s.reqs, req)
	rec := llm.CallRecord{Model: llm.ModelHaiku45, Tier: req.Tier, Purpose: req.Purpose, CostUSD: s.cost, LatencyMS: 5}
	trace.Append(rec)
	return s.msgs[min(i, len(s.msgs)-1)], rec, nil
}

func toolMessage(t *testing.T, name string, input any) *anthropic.Message {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","stop_reason":"tool_use","content":[{"type":"tool_use","id":"tu_1","name":%q,"input":%s}],"usage":{"input_tokens":1,"output_tokens":1}}`, name, raw)
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatal(err)
	}
	return &msg
}

func judgeMsg(t *testing.T, verdict, confidence string) *anthropic.Message {
	return toolMessage(t, ToolJudge, map[string]string{"verdict": verdict, "confidence": confidence, "reason": "values match"})
}

func supervisorMsg(t *testing.T, decision string) *anthropic.Message {
	return toolMessage(t, ToolSupervise, map[string]string{"decision": decision, "reason": "rubric applied right"})
}

func testInput() Input {
	return Input{
		QuestionID: "e03",
		Question:   "How many distribution centers are there?",
		Rubric:     Rubric{ID: "e03", CorrectAnswer: "10", FailureCriteria: "any other count", CloseButWrong: "names instead of a count"},
		Expected:   float64(10),
		Answer:     "10",
		SQL:        "SELECT COUNT(*) FROM distribution_centers",
	}
}

func TestLoadRubrics(t *testing.T) {
	rubrics, err := LoadRubrics(filepath.Join("..", "..", "..", "bench", RubricsFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(rubrics) != 32 {
		t.Fatalf("rubrics %d, want 32", len(rubrics))
	}
	for id, r := range rubrics {
		if r.ID != id {
			t.Errorf("key %q holds rubric %q", id, r.ID)
		}
	}
}

func TestParseJudgment(t *testing.T) {
	j, err := ParseJudgment(judgeMsg(t, " Pass ", " High "))
	if err != nil {
		t.Fatal(err)
	}
	if j.Verdict != VerdictPass || j.Confidence != ConfidenceHigh || j.Reason != "values match" {
		t.Errorf("judgment %+v", j)
	}
	for name, msg := range map[string]*anthropic.Message{
		"bad verdict":    judgeMsg(t, "maybe", "high"),
		"bad confidence": judgeMsg(t, "pass", "certain"),
		"wrong tool":     toolMessage(t, "other", map[string]string{"verdict": "pass", "confidence": "high", "reason": "x"}),
	} {
		if _, err := ParseJudgment(msg); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := ParseJudgment(nil); err == nil {
		t.Error("nil message: want error")
	}
}

func TestParseSupervision(t *testing.T) {
	s, err := ParseSupervision(supervisorMsg(t, " Overturn "))
	if err != nil {
		t.Fatal(err)
	}
	if s.Decision != DecisionOverturn {
		t.Errorf("supervision %+v", s)
	}
	if _, err := ParseSupervision(supervisorMsg(t, "maybe")); err == nil {
		t.Error("bad decision: want error")
	}
	if _, err := ParseSupervision(nil); err == nil {
		t.Error("nil message: want error")
	}
}

func TestToolsAreStrict(t *testing.T) {
	jt := JudgeTools()[0].OfTool
	if jt.Name != ToolJudge || !jt.Strict.Value {
		t.Errorf("judge tool %+v", jt)
	}
	props := jt.InputSchema.Properties.(map[string]any)
	if got := props["verdict"].(map[string]any)["enum"]; fmt.Sprint(got) != "[pass fail]" {
		t.Errorf("verdict enum %v", got)
	}
	if got := props["confidence"].(map[string]any)["enum"]; fmt.Sprint(got) != "[high medium low]" {
		t.Errorf("confidence enum %v", got)
	}
	st := SupervisorTools()[0].OfTool
	if st.Name != ToolSupervise || !st.Strict.Value {
		t.Errorf("supervisor tool %+v", st)
	}
	sprops := st.InputSchema.Properties.(map[string]any)
	if got := sprops["decision"].(map[string]any)["enum"]; fmt.Sprint(got) != "[agree overturn]" {
		t.Errorf("decision enum %v", got)
	}
}

func TestNeedsSupervisor(t *testing.T) {
	cases := map[Judgment]bool{
		{Verdict: VerdictFail, Confidence: ConfidenceHigh}:   true,
		{Verdict: VerdictFail, Confidence: ConfidenceLow}:    true,
		{Verdict: VerdictPass, Confidence: ConfidenceLow}:    true,
		{Verdict: VerdictPass, Confidence: ConfidenceMedium}: false,
		{Verdict: VerdictPass, Confidence: ConfidenceHigh}:   false,
	}
	for j, want := range cases {
		if got := NeedsSupervisor(j, nil); got != want {
			t.Errorf("%+v: supervised %v, want %v", j, got, want)
		}
	}
	yes, no := true, false
	for _, c := range []struct {
		j      Judgment
		grader *bool
		want   bool
	}{
		{Judgment{Verdict: VerdictPass, Confidence: ConfidenceHigh}, &no, true},
		{Judgment{Verdict: VerdictPass, Confidence: ConfidenceMedium}, &no, true},
		{Judgment{Verdict: VerdictPass, Confidence: ConfidenceHigh}, &yes, false},
		{Judgment{Verdict: VerdictPass, Confidence: ConfidenceLow}, &yes, true},
		{Judgment{Verdict: VerdictFail, Confidence: ConfidenceHigh}, &yes, true},
		{Judgment{Verdict: VerdictFail, Confidence: ConfidenceHigh}, &no, true},
	} {
		if got := NeedsSupervisor(c.j, c.grader); got != c.want {
			t.Errorf("%+v grader %v: supervised %v, want %v", c.j, *c.grader, got, c.want)
		}
	}
}

func TestJudgePassReviewedWhenGraderFails(t *testing.T) {
	p := &seqProvider{msgs: []*anthropic.Message{judgeMsg(t, VerdictPass, ConfidenceHigh), supervisorMsg(t, DecisionOverturn)}, cost: 0.001}
	in := testInput()
	no := false
	in.GraderCorrect = &no
	out, err := New(p).JudgeOne(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if out.FinalPass || !out.Supervised || p.calls != 2 {
		t.Errorf("pass the grader failed must be reviewed and can be overturned: %+v calls %d", out, p.calls)
	}
	agree := &seqProvider{msgs: []*anthropic.Message{judgeMsg(t, VerdictPass, ConfidenceHigh)}, cost: 0.001}
	yes := true
	in.GraderCorrect = &yes
	out, err = New(agree).JudgeOne(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !out.FinalPass || out.Supervised || agree.calls != 1 {
		t.Errorf("pass the grader agrees with must skip review: %+v calls %d", out, agree.calls)
	}
}

func TestJudgePassHighSkipsSupervisor(t *testing.T) {
	stub := &stubProvider{msg: judgeMsg(t, VerdictPass, ConfidenceHigh), cost: 0.0012}
	out, err := New(stub).JudgeOne(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if !out.FinalPass || out.Supervised || out.Supervision.Decision != "" {
		t.Errorf("outcome %+v", out)
	}
	if out.CostUSD != 0.0012 || len(out.Records) != 1 {
		t.Errorf("cost %v records %d", out.CostUSD, len(out.Records))
	}
	if stub.req.Tier != llm.TierCheap || stub.req.Purpose != "bench-judge-e03" {
		t.Errorf("request tier %s purpose %s", stub.req.Tier, stub.req.Purpose)
	}
}

func TestJudgeFailReviewedAndAgreed(t *testing.T) {
	p := &seqProvider{msgs: []*anthropic.Message{judgeMsg(t, VerdictFail, ConfidenceHigh), supervisorMsg(t, DecisionAgree)}, cost: 0.001}
	out, err := New(p).JudgeOne(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if out.FinalPass || !out.Supervised || out.Supervision.Decision != DecisionAgree {
		t.Errorf("outcome %+v", out)
	}
	if p.calls != 2 || out.CostUSD != 0.002 || len(out.Records) != 2 {
		t.Errorf("calls %d cost %v records %d", p.calls, out.CostUSD, len(out.Records))
	}
	if p.reqs[0].Tier != llm.TierCheap || p.reqs[1].Tier != llm.TierMid {
		t.Errorf("tiers %s %s, want cheap then mid", p.reqs[0].Tier, p.reqs[1].Tier)
	}
	if p.reqs[1].Purpose != "bench-judge-supervisor-e03" {
		t.Errorf("supervisor purpose %s", p.reqs[1].Purpose)
	}
}

func TestJudgeOverturnFlipsVerdict(t *testing.T) {
	p := &seqProvider{msgs: []*anthropic.Message{judgeMsg(t, VerdictFail, ConfidenceLow), supervisorMsg(t, DecisionOverturn)}, cost: 0.001}
	out, err := New(p).JudgeOne(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if !out.FinalPass || !out.Supervised {
		t.Errorf("overturn must flip fail to pass: %+v", out)
	}
}

func TestJudgeLowConfidencePassReviewed(t *testing.T) {
	p := &seqProvider{msgs: []*anthropic.Message{judgeMsg(t, VerdictPass, ConfidenceLow), supervisorMsg(t, DecisionAgree)}, cost: 0.001}
	out, err := New(p).JudgeOne(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if !out.FinalPass || !out.Supervised || p.calls != 2 {
		t.Errorf("low-confidence pass must be reviewed: %+v", out)
	}
}

func TestJudgeMediumPassSkipsSupervisor(t *testing.T) {
	p := &seqProvider{msgs: []*anthropic.Message{judgeMsg(t, VerdictPass, ConfidenceMedium)}, cost: 0.001}
	out, err := New(p).JudgeOne(context.Background(), testInput())
	if err != nil {
		t.Fatal(err)
	}
	if !out.FinalPass || out.Supervised || p.calls != 1 {
		t.Errorf("medium pass must skip review: %+v", out)
	}
}

func TestJudgeErrorKeepsCost(t *testing.T) {
	stub := &stubProvider{err: errors.New("boom"), cost: 0.002}
	out, err := New(stub).JudgeOne(context.Background(), testInput())
	if err == nil {
		t.Fatal("want error")
	}
	if out.CostUSD != 0.004 || len(out.Records) != 2 {
		t.Errorf("two attempts must both be recorded: %+v", out)
	}
}

func TestSummarizeAgreement(t *testing.T) {
	lines := []Line{
		{ID: "e01", Lenient: true, Strict: true, Final: true, RouteCostUSD: 0.001},
		{ID: "m02", Lenient: true, Strict: false, Final: true, RouteCostUSD: 0.001},
		{ID: "x01", Lenient: false, Strict: false, Final: true, JudgeReason: "close enough", Supervised: true, SupervisorDecision: DecisionOverturn, RouteCostUSD: 0.003},
		{ID: "x02", Error: "boom", RouteCostUSD: 0.004},
	}
	s := Summarize("cfg", "src", "stamp", lines)
	if s.Total != 4 || s.Errors != 1 || s.JudgePass != 3 || s.Supervised != 1 || s.Overturns != 1 {
		t.Errorf("summary %+v", s)
	}
	if s.AgreeLenient != 2 || s.AgreeStrict != 1 {
		t.Errorf("agree lenient %d strict %d", s.AgreeLenient, s.AgreeStrict)
	}
	if s.AgreementLenient != 2.0/3 || s.AgreementStrict != 1.0/3 {
		t.Errorf("rates %v %v, want agreement over the 3 error-free lines", s.AgreementLenient, s.AgreementStrict)
	}
	if math.Abs(s.JudgeCostUSD-0.009) > 1e-12 {
		t.Errorf("cost %v, want 0.009 including the errored line", s.JudgeCostUSD)
	}
	if text := s.Format(); !strings.Contains(text, "errors=1") || !strings.Contains(text, "(2/3)") {
		t.Errorf("format lacks errors or error-free denominator:\n%s", text)
	}
	if len(s.Disagreements) != 2 || s.Disagreements[0].ID != "m02" || s.Disagreements[1].ID != "x01" {
		t.Errorf("disagreements %+v", s.Disagreements)
	}
}

func TestFormatExpected(t *testing.T) {
	if got := FormatExpected("abc"); got != "abc" {
		t.Errorf("string %q", got)
	}
	if got := FormatExpected(float64(149.95)); got != "149.95" {
		t.Errorf("number %q", got)
	}
	if got := FormatExpected([]any{"a", "b"}); got != "a\nb" {
		t.Errorf("list %q", got)
	}
	table := map[string]any{
		"columns": []any{"month", "orders"},
		"rows":    []any{[]any{"2024-01", "3417"}, []any{"2024-02", "3468"}},
	}
	if got := FormatExpected(table); got != "month | orders\n2024-01 | 3417\n2024-02 | 3468" {
		t.Errorf("table %q", got)
	}
}

func TestJudgePromptCarriesRubricAndSQL(t *testing.T) {
	prompt := testInput().JudgePrompt()
	for _, want := range []string{"How many distribution centers", "10", "any other count", "names instead of a count", "SELECT COUNT(*)"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt misses %q:\n%s", want, prompt)
		}
	}
	super := testInput().SupervisorPrompt(Judgment{Verdict: VerdictFail, Confidence: ConfidenceHigh, Reason: "wrong count"})
	if !strings.Contains(super, "First judge verdict: fail") || !strings.Contains(super, "wrong count") {
		t.Errorf("supervisor prompt:\n%s", super)
	}
}
