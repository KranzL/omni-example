package router

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/llm"
)

type tierAgent struct {
	results map[llm.Tier]agent.Result
	errs    map[llm.Tier][]error
	calls   []llm.Tier
}

func (f *tierAgent) Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error) {
	n := 0
	for _, c := range f.calls {
		if c == tier {
			n++
		}
	}
	f.calls = append(f.calls, tier)
	if errs := f.errs[tier]; n < len(errs) && errs[n] != nil {
		return agent.Result{Tier: tier, CostUSD: 0.001}, errs[n]
	}
	res := f.results[tier]
	res.Tier = tier
	return res, nil
}

func good(cost float64) agent.Result {
	return agent.Result{
		Answer:     "42",
		SQL:        "SELECT count(*)\nFROM users",
		Confidence: "high",
		Submitted:  true,
		Turns:      2,
		Steps:      []agent.SQLStep{{Turn: 1, SQL: "SELECT count(*) FROM users", Rows: 1, Output: "rows: 1\ncapped: false\n\ncount\n42\n"}},
		Usage:      llm.Usage{InputTokens: 10, OutputTokens: 5, CacheCreationInputTokens: 7000},
		CostUSD:    cost,
		WallMS:     100,
	}
}

func allGood() map[llm.Tier]agent.Result {
	return map[llm.Tier]agent.Result{llm.TierCheap: good(0.01), llm.TierMid: good(0.02), llm.TierTop: good(0.04)}
}

type seqProvider struct {
	msgs  []*anthropic.Message
	errs  []error
	reqs  []llm.Request
	cost  float64
	calls int
}

func (s *seqProvider) Name() string { return "seq" }

func (s *seqProvider) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	i := s.calls
	s.calls++
	s.reqs = append(s.reqs, req)
	rec := llm.CallRecord{Model: llm.ModelHaiku45, Tier: req.Tier, Purpose: req.Purpose, CostUSD: s.cost, Usage: llm.Usage{InputTokens: 3, CacheReadInputTokens: 7000}}
	trace.Append(rec)
	var err error
	if i < len(s.errs) {
		err = s.errs[i]
	}
	return s.msgs[min(i, len(s.msgs)-1)], rec, err
}

func verdictMsg(t *testing.T, verdict string) *anthropic.Message {
	return toolMessage(t, ToolVerdict, map[string]string{"verdict": verdict, "reason": "because"})
}

func tiersOf(o Outcome) []llm.Tier {
	var out []llm.Tier
	for _, a := range o.Attempts {
		out = append(out, a.Tier)
	}
	return out
}

func sameTiers(a, b []llm.Tier) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCascadeStopsOnCheapWithoutSignals(t *testing.T) {
	fake := &tierAgent{results: allGood()}
	o, err := NewCascade(NameCascadeSignals, fake, nil).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if !sameTiers(tiersOf(o), []llm.Tier{llm.TierCheap}) || o.Final().Tier != llm.TierCheap {
		t.Fatalf("path %s", o.Path())
	}
	if o.AgentCost() != 0.01 || o.OverheadCost() != 0 || o.Reason() != "cheap: final" {
		t.Errorf("outcome %+v reason %q", o, o.Reason())
	}
}

func TestCascadeEachSignalEscalates(t *testing.T) {
	cases := map[string]func(r *agent.Result){
		SignalLowConfidence: func(r *agent.Result) { r.Confidence = "low" },
		SignalNoSubmit: func(r *agent.Result) {
			r.Submitted = false
			r.Answer = "prose"
		},
		SignalSQLErrors: func(r *agent.Result) { r.SQLErrors = 3 },
		SignalZeroRows:  func(r *agent.Result) { r.Steps[0].Rows = 0 },
	}
	for signal, mutate := range cases {
		results := allGood()
		cheap := results[llm.TierCheap]
		cheap.Steps = append([]agent.SQLStep(nil), cheap.Steps...)
		mutate(&cheap)
		results[llm.TierCheap] = cheap
		fake := &tierAgent{results: results}
		o, err := NewCascade(NameCascadeSignals, fake, nil).Solve(context.Background(), "q")
		if err != nil {
			t.Fatal(err)
		}
		if !sameTiers(tiersOf(o), []llm.Tier{llm.TierCheap, llm.TierMid}) {
			t.Errorf("%s: path %s", signal, o.Path())
			continue
		}
		if got := o.Attempts[0].Signals; len(got) != 1 || got[0] != signal {
			t.Errorf("%s: fired %v", signal, got)
		}
		if math.Abs(o.AgentCost()-0.03) > 1e-12 {
			t.Errorf("%s: abandoned cheap attempt must be charged, agent cost %v", signal, o.AgentCost())
		}
		if o.Reason() != "cheap: "+signal+"; mid: final" {
			t.Errorf("%s: reason %q", signal, o.Reason())
		}
	}
}

func TestSignalsFiredDetails(t *testing.T) {
	s := DefaultSignals()
	r := good(0)
	if got := s.Fired(r); len(got) != 0 {
		t.Errorf("clean result fired %v", got)
	}
	r.Confidence = "medium"
	if got := s.Fired(r); len(got) != 0 {
		t.Errorf("medium confidence must not fire: %v", got)
	}
	r.Confidence = ""
	if got := s.Fired(r); len(got) != 1 || got[0] != SignalLowConfidence {
		t.Errorf("missing confidence counts as low: %v", got)
	}
	r = good(0)
	r.Nudged = true
	if got := s.Fired(r); len(got) != 1 || got[0] != SignalNoSubmit {
		t.Errorf("nudged at the turn limit fires no_submit even when it then submitted: %v", got)
	}
	r = good(0)
	r.SQLErrors = 2
	if got := s.Fired(r); len(got) != 0 {
		t.Errorf("two SQL errors is not more than two: %v", got)
	}
	r = good(0)
	r.Steps = []agent.SQLStep{{SQL: "SELECT count(*) FROM users;", Error: "relation does not exist"}}
	if got := s.Fired(r); len(got) != 1 || got[0] != SignalZeroRows {
		t.Errorf("submitted SQL that errored counts as zero rows: %v", got)
	}
	r = good(0)
	r.Steps = []agent.SQLStep{{SQL: "SELECT 1", Rows: 0}, {SQL: "SELECT 2", Rows: 5}}
	if got := s.Fired(r); len(got) != 0 {
		t.Errorf("unmatched SQL falls back to the last successful step: %v", got)
	}
	r = good(0)
	r.Confidence = "low"
	r.SQLErrors = 5
	r.Steps[0].Rows = 0
	all := s.Fired(r)
	if strings.Join(all, ",") != "low_confidence,sql_errors,zero_rows" {
		t.Errorf("several signals: %v", all)
	}
	if got := (Signals{}).Fired(r); len(got) != 0 {
		t.Errorf("all signals switched off must fire nothing: %v", got)
	}
	for name, off := range map[string]Signals{
		SignalLowConfidence: {NoSubmit: true, SQLErrors: true, ZeroRows: true},
		SignalSQLErrors:     {LowConfidence: true, NoSubmit: true, ZeroRows: true},
		SignalZeroRows:      {LowConfidence: true, NoSubmit: true, SQLErrors: true},
	} {
		for _, f := range off.Fired(r) {
			if f == name {
				t.Errorf("%s fired while switched off", name)
			}
		}
	}
	s.MaxSQLErrors = 5
	for _, f := range s.Fired(r) {
		if f == SignalSQLErrors {
			t.Error("max_sql_errors 5 must not fire on 5 errors")
		}
	}
}

func TestCascadeClimbsToTopAndNeverChecksTop(t *testing.T) {
	results := allGood()
	for _, tier := range llm.Tiers {
		r := results[tier]
		r.Confidence = "low"
		results[tier] = r
	}
	fake := &tierAgent{results: results}
	stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictAccept)}, cost: 0.002}
	o, err := NewCascade(NameCascadeVerifyHaiku, fake, NewVerifier(stub, llm.TierCheap, "SEM", nil)).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if !sameTiers(tiersOf(o), llm.Tiers) || o.Final().Tier != llm.TierTop {
		t.Fatalf("path %s", o.Path())
	}
	if len(o.Final().Signals) != 0 || o.Final().Verified {
		t.Errorf("top attempt must not be checked %+v", o.Final())
	}
	if stub.calls != 0 {
		t.Errorf("verifier must not run when another signal already fired, calls %d", stub.calls)
	}
	if math.Abs(o.AgentCost()-0.07) > 1e-12 {
		t.Errorf("agent cost %v, want 0.07", o.AgentCost())
	}
	var writes int64
	for _, a := range o.Attempts {
		writes += a.Result.Usage.CacheCreationInputTokens
	}
	if writes != 21000 || o.AgentUsage().CacheCreationInputTokens != 21000 {
		t.Errorf("each tier writes its own cache: %d", writes)
	}
}

func TestCascadeVerifierRejectEscalates(t *testing.T) {
	fake := &tierAgent{results: allGood()}
	stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictReject), verdictMsg(t, VerdictAccept)}, cost: 0.002}
	v := NewVerifier(stub, llm.TierMid, "SEM", nil)
	o, err := NewCascade(NameCascadeVerifySonnet, fake, v).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if !sameTiers(tiersOf(o), []llm.Tier{llm.TierCheap, llm.TierMid}) {
		t.Fatalf("path %s", o.Path())
	}
	cheap, mid := o.Attempts[0], o.Attempts[1]
	if !cheap.Verified || cheap.Verdict.Verdict != VerdictReject || len(cheap.Signals) != 1 || cheap.Signals[0] != SignalVerifierReject {
		t.Errorf("cheap attempt %+v", cheap)
	}
	if !mid.Verified || mid.Verdict.Verdict != VerdictAccept || mid.Escalate() {
		t.Errorf("mid attempt %+v", mid)
	}
	if math.Abs(o.OverheadCost()-0.004) > 1e-12 || o.OverheadUsage().CacheReadInputTokens != 14000 {
		t.Errorf("verifier overhead %v %+v", o.OverheadCost(), o.OverheadUsage())
	}
	if o.Reason() != "cheap: verifier_reject; mid: accept" {
		t.Errorf("reason %q", o.Reason())
	}
	req := stub.reqs[0]
	if req.Tier != llm.TierMid || req.Purpose != "verifier" || len(req.Tools) != 1 {
		t.Errorf("verifier request %+v", req)
	}
	if len(req.System) != 2 || req.System[1].Text != "SEM" || req.System[1].CacheControl.Type == "" {
		t.Errorf("verifier system must end with the cached semantic layer: %+v", req.System)
	}
	prompt := req.Messages[0].Content[0].OfText.Text
	for _, want := range []string{"Question:\nq", "SELECT count(*)\nFROM users", "count\n42", "Agent's answer:\n42"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("verifier prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestCascadeVerifierAcceptStops(t *testing.T) {
	fake := &tierAgent{results: allGood()}
	stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictAccept)}, cost: 0.001}
	o, err := NewCascade(NameCascadeVerifyHaiku, fake, NewVerifier(stub, llm.TierCheap, "SEM", nil)).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Attempts) != 1 || !o.Final().Verified || o.OverheadCost() != 0.001 {
		t.Errorf("outcome %+v", o)
	}
}

func TestCascadeVerifierSwitchedOff(t *testing.T) {
	fake := &tierAgent{results: allGood()}
	stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictReject)}}
	c := NewCascade(NameCascadeVerifyHaiku, fake, NewVerifier(stub, llm.TierCheap, "SEM", nil))
	c.Signals.Verifier = false
	o, err := c.Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Attempts) != 1 || stub.calls != 0 {
		t.Errorf("verifier switched off still ran: %+v calls %d", o, stub.calls)
	}
}

func TestCascadeVerifierFailureEscalates(t *testing.T) {
	var text anthropic.Message
	text.StopReason = anthropic.StopReasonEndTurn
	fake := &tierAgent{results: allGood()}
	stub := &seqProvider{msgs: []*anthropic.Message{&text, &text, verdictMsg(t, VerdictAccept)}, cost: 0.001}
	o, err := NewCascade(NameCascadeVerifyHaiku, fake, NewVerifier(stub, llm.TierCheap, "SEM", nil)).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls != 3 {
		t.Errorf("verifier must retry once on cheap and run once on mid, calls %d", stub.calls)
	}
	if !sameTiers(tiersOf(o), []llm.Tier{llm.TierCheap, llm.TierMid}) {
		t.Fatalf("a verifier error must escalate, path %s", o.Path())
	}
	cheap := o.Attempts[0]
	if !cheap.Verified || cheap.Verdict.Verdict != VerdictError || cheap.VerifyCost != 0.002 {
		t.Errorf("cheap attempt %+v", cheap)
	}
	if len(cheap.Signals) != 1 || cheap.Signals[0] != SignalVerifierError {
		t.Errorf("cheap signals %v, want %s", cheap.Signals, SignalVerifierError)
	}
	if o.Final().Tier != llm.TierMid || o.Reason() != "cheap: verifier_error; mid: accept" {
		t.Errorf("final %s reason %q", o.Final().Tier, o.Reason())
	}
}

func TestCascadeAgentErrorRetriesThenEscalates(t *testing.T) {
	boom := errors.New("overloaded")
	fake := &tierAgent{results: allGood(), errs: map[llm.Tier][]error{llm.TierCheap: {boom, boom}}}
	o, err := NewCascade(NameCascadeSignals, fake, nil).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if !sameTiers(fake.calls, []llm.Tier{llm.TierCheap, llm.TierCheap, llm.TierMid}) {
		t.Errorf("calls %v", fake.calls)
	}
	if o.Attempts[0].Err == "" || o.Attempts[0].Signals[0] != SignalAgentError || o.Final().Tier != llm.TierMid {
		t.Errorf("outcome %+v", o)
	}
	fake = &tierAgent{results: allGood(), errs: map[llm.Tier][]error{llm.TierCheap: {boom}}}
	o, err = NewCascade(NameCascadeSignals, fake, nil).Solve(context.Background(), "q")
	if err != nil || len(o.Attempts) != 1 || o.Final().Tier != llm.TierCheap {
		t.Errorf("one transient error is retried on the same tier: %+v %v", o, err)
	}
	fake = &tierAgent{results: allGood(), errs: map[llm.Tier][]error{llm.TierTop: {boom, boom}}}
	c := NewCascade(NameCascadeSignals, fake, nil)
	c.Tiers = []llm.Tier{llm.TierTop}
	if _, err := c.Solve(context.Background(), "q"); err == nil {
		t.Error("an error on the last tier must be returned")
	}
}

func TestCascadeRetryKeepsFailedSpend(t *testing.T) {
	boom := errors.New("overloaded")
	fake := &tierAgent{results: allGood(), errs: map[llm.Tier][]error{llm.TierCheap: {boom}}}
	o, err := NewCascade(NameCascadeSignals, fake, nil).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Attempts) != 1 {
		t.Fatalf("path %s", o.Path())
	}
	if math.Abs(o.AgentCost()-0.011) > 1e-12 {
		t.Errorf("agent cost %v, want 0.011 including the failed try", o.AgentCost())
	}
	fake = &tierAgent{results: allGood(), errs: map[llm.Tier][]error{llm.TierCheap: {boom, boom}}}
	o, err = NewCascade(NameCascadeSignals, fake, nil).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(o.AgentCost()-0.022) > 1e-12 {
		t.Errorf("agent cost %v, want 0.022 with both failed cheap tries", o.AgentCost())
	}
}

func TestAddSpendMergesFailedTry(t *testing.T) {
	failed := agent.Result{CostUSD: 0.003, WallMS: 50, CacheRead: 1, CacheCreation: 2,
		Usage:   llm.Usage{InputTokens: 4, OutputTokens: 1},
		Records: []llm.CallRecord{{Purpose: "failed"}}}
	got := addSpend(good(0.01), failed)
	if math.Abs(got.CostUSD-0.013) > 1e-12 || got.WallMS != 150 || got.CacheRead != 1 || got.CacheCreation != 2 {
		t.Errorf("merged %+v", got)
	}
	if got.Usage.InputTokens != 14 || got.Usage.OutputTokens != 6 || got.Usage.CacheCreationInputTokens != 7000 {
		t.Errorf("usage %+v", got.Usage)
	}
	if len(got.Records) != 1 || got.Records[0].Purpose != "failed" || got.Answer != "42" {
		t.Errorf("records %+v answer %q", got.Records, got.Answer)
	}
}

func TestCascadeLastTierErrorKeepsEarlierAnswer(t *testing.T) {
	boom := errors.New("overloaded")
	results := allGood()
	cheap := results[llm.TierCheap]
	cheap.Confidence = "low"
	cheap.Answer = "cheap answer"
	results[llm.TierCheap] = cheap
	fake := &tierAgent{results: results, errs: map[llm.Tier][]error{llm.TierMid: {boom, boom}}}
	c := NewCascade(NameCascadeSignals, fake, nil)
	c.Tiers = []llm.Tier{llm.TierCheap, llm.TierMid}
	o, err := c.Solve(context.Background(), "q")
	if err != nil {
		t.Fatalf("an earlier answer exists, err %v", err)
	}
	if len(o.Attempts) != 2 || o.Attempts[1].Err == "" {
		t.Fatalf("the failed mid attempt must stay recorded: %+v", o.Attempts)
	}
	if f := o.Final(); f.Tier != llm.TierCheap || f.Result.Answer != "cheap answer" || o.FinalIndex() != 0 {
		t.Errorf("final %+v", f)
	}
	if o.Reason() != "cheap: low_confidence; mid: agent_error" {
		t.Errorf("reason %q", o.Reason())
	}
}

type fakeQuerier struct {
	sql string
}

func (f *fakeQuerier) Query(ctx context.Context, sql string, maxRows int) (db.Result, error) {
	f.sql = sql
	return db.Result{Columns: []string{"n"}, Rows: [][]string{{"7"}}}, nil
}

func TestVerifierRerunsUnmatchedSQL(t *testing.T) {
	q := &fakeQuerier{}
	stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictAccept)}}
	v := NewVerifier(stub, llm.TierCheap, "SEM", q)
	r := good(0)
	r.SQL = "SELECT 7 AS n"
	if _, err := v.Verify(context.Background(), "q", r); err != nil {
		t.Fatal(err)
	}
	if q.sql != "SELECT 7 AS n" || !strings.Contains(stub.reqs[0].Messages[0].Content[0].OfText.Text, "n\n7") {
		t.Errorf("rerun sql %q", q.sql)
	}
	q.sql = ""
	if _, err := v.Verify(context.Background(), "q", good(0)); err != nil || q.sql != "" {
		t.Errorf("matched SQL must use the agent's own rows, rerun %q %v", q.sql, err)
	}
}

func TestParseVerdictRejectsUnknown(t *testing.T) {
	if _, err := ParseVerdict(toolMessage(t, ToolVerdict, map[string]string{"verdict": "maybe", "reason": "x"})); err == nil {
		t.Error("unknown verdict: want error")
	}
	v, err := ParseVerdict(toolMessage(t, ToolVerdict, map[string]string{"verdict": " Reject ", "reason": " bad filter "}))
	if err != nil || v.Verdict != VerdictReject || v.Reason != "bad filter" {
		t.Errorf("verdict %+v %v", v, err)
	}
}

func TestCascadeNames(t *testing.T) {
	for _, n := range CascadeNames() {
		if !IsCascade(n) || ValidName(n) != nil {
			t.Errorf("%s must be a valid cascade name", n)
		}
	}
	if IsCascade(NameClassifier) {
		t.Error("classifier is not a cascade")
	}
	if tier, ok := CascadeVerifierTier(NameCascadeVerifySonnet); !ok || tier != llm.TierMid {
		t.Errorf("sonnet verifier tier %s", tier)
	}
	if _, ok := CascadeVerifierTier(NameCascadeSignals); ok {
		t.Error("cascade-signals has no verifier")
	}
}
