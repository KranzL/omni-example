package router

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/jev"
	"github.com/KranzL/omni-example/internal/llm"
)

func requestHash(t *testing.T, req llm.Request) string {
	t.Helper()
	b, err := json.Marshal(struct{ S, M, T any }{req.System, req.Messages, req.Tools})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func valueHash(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func classifierRequest(t *testing.T, opts PromptOptions) llm.Request {
	t.Helper()
	stub := &stubProvider{msg: toolMessage(t, ToolClassify, map[string]string{"label": "easy", "reason": "r"})}
	if _, err := NewClassifierWith(stub, "SEMANTIC", opts).Decide(context.Background(), DecisionInput{Question: "Q?"}); err != nil {
		t.Fatal(err)
	}
	return stub.req
}

func verifierRequest(t *testing.T, opts PromptOptions, evidence string) llm.Request {
	t.Helper()
	stub := &stubProvider{msg: toolMessage(t, ToolVerdict, map[string]string{"verdict": "accept", "reason": "r"})}
	v := NewVerifierWith(stub, llm.TierCheap, "SEMANTIC", nil, opts)
	v.Evidence = evidence
	if _, err := v.Verify(context.Background(), "Q?", agent.Result{SQL: "SELECT 1", Answer: "1"}); err != nil {
		t.Fatal(err)
	}
	return stub.req
}

func TestDefaultRouterRequestsUnchanged(t *testing.T) {
	want := map[string]string{
		"classifier": "52a5b2899ef2cddf9333cc7d7f05f2901bc898d029ac5656f03836088a11fc7e",
		"verifier":   "f85ee7c0a9f5f650aa032b876b739aeb8241f51493205fe0baab5a5c77b94e12",
		"jevdiff":    "f729a6167cc79a4d28d1557dbf1a7079f2c15e204874d042fb74566d5df931c5",
		"jevverdict": "994f23c70b8f29ed2f838fd484d2c82dc820b3f0e877fc84460ba2b7df5ba300",
	}
	cs := &stubProvider{msg: toolMessage(t, ToolClassify, map[string]string{"label": "easy", "reason": "r"})}
	if _, err := NewClassifier(cs, "SEMANTIC").Decide(context.Background(), DecisionInput{Question: "Q?"}); err != nil {
		t.Fatal(err)
	}
	vs := &stubProvider{msg: toolMessage(t, ToolVerdict, map[string]string{"verdict": "accept", "reason": "r"})}
	if _, err := NewVerifier(vs, llm.TierCheap, "SEMANTIC", nil).Verify(context.Background(), "Q?", agent.Result{SQL: "SELECT 1", Answer: "1"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{
		"classifier": requestHash(t, cs.req),
		"verifier":   requestHash(t, vs.req),
		"jevdiff":    valueHash(t, JevDifficultyQuestions()),
		"jevverdict": valueHash(t, JevVerdictQuestions()),
	}
	got2 := map[string]string{
		"classifier": requestHash(t, classifierRequest(t, PromptOptions{Domain: DomainEcommerce})),
		"verifier":   requestHash(t, verifierRequest(t, PromptOptions{Domain: DomainEcommerce}, "")),
		"jevdiff":    valueHash(t, JevDifficultyQuestionsFor(DomainEcommerce)),
		"jevverdict": got["jevverdict"],
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s default request changed: %s", k, got[k])
		}
		if got2[k] != w {
			t.Errorf("%s explicit ecommerce request differs from default: %s", k, got2[k])
		}
	}
}

func TestRouterNoCacheDropsCacheControl(t *testing.T) {
	for name, req := range map[string]llm.Request{
		"classifier": classifierRequest(t, PromptOptions{NoCache: true}),
		"verifier":   verifierRequest(t, PromptOptions{NoCache: true}, ""),
	} {
		b, err := json.Marshal(struct{ S, M, T any }{req.System, req.Messages, req.Tools})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "cache_control") {
			t.Errorf("%s no-cache request carries cache_control: %s", name, b)
		}
		if len(req.System) != 2 || req.System[1].Text != "SEMANTIC" {
			t.Errorf("%s no-cache system %+v", name, req.System)
		}
	}
	on := classifierRequest(t, PromptOptions{})
	if on.System[1].CacheControl.Type == "" {
		t.Error("default classifier request must keep cache_control")
	}
}

func TestBirdDomainWording(t *testing.T) {
	texts := map[string]string{
		"classifier": ClassifierInstructionsFor(DomainBird),
		"verifier":   VerifierInstructionsFor(DomainBird),
		"jev":        JevDifficultyQuestionsFor(DomainBird)[JevQuestionDifficulty].Instructions.(string),
	}
	for name, s := range texts {
		if strings.Contains(s, "ecommerce") || strings.Contains(s, "Postgres") || !strings.Contains(s, "SQLite database") {
			t.Errorf("%s bird wording: %.200s", name, s)
		}
	}
	if !strings.HasPrefix(texts["classifier"], "You route analytics questions about a SQLite database from the BIRD benchmark to one of three model tiers.") {
		t.Errorf("classifier bird head: %.160s", texts["classifier"])
	}
	req := classifierRequest(t, PromptOptions{Domain: DomainBird})
	if req.System[0].Text != texts["classifier"] {
		t.Error("NewClassifierWith must use the domain wording")
	}
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelEasy, 0.99)}}
	if _, err := NewJevRouterFor(f, NewClassifierWith(&stubProvider{}, "S", PromptOptions{Domain: DomainBird}), "S", 0, DomainBird).Route(context.Background(), "Q"); err != nil {
		t.Fatal(err)
	}
	if f.questions[JevQuestionDifficulty].Instructions != texts["jev"] {
		t.Errorf("jev router question %q", f.questions[JevQuestionDifficulty].Instructions)
	}
}

func TestVerifierSeesEvidence(t *testing.T) {
	text := func(req llm.Request) string { return req.Messages[0].Content[0].OfText.Text }
	with := text(verifierRequest(t, PromptOptions{}, "  revenue means price times quantity  "))
	if !strings.HasPrefix(with, "Question:\nQ?\n\nContext\nrevenue means price times quantity\n\nSubmitted SQL:") {
		t.Errorf("evidence prompt %q", with)
	}
	without := text(verifierRequest(t, PromptOptions{}, ""))
	if strings.Contains(without, "Context") || !strings.HasPrefix(without, "Question:\nQ?\n\nSubmitted SQL:") {
		t.Errorf("prompt without evidence %q", without)
	}
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(VerdictAccept, 0.99)}}
	v := NewVerifier(&seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictAccept)}}, llm.TierCheap, "S", nil)
	v.Evidence = "hint"
	g := NewJevGatedVerifier(f, v, "S", 0)
	if _, err := g.Verify(context.Background(), "Q?", agent.Result{SQL: "SELECT 1", Answer: "1"}); err != nil {
		t.Fatal(err)
	}
	if q := f.state.(map[string]any)["question"]; q != "Q?\n\nContext\nhint" {
		t.Errorf("jev verifier question %q", q)
	}
}

func TestGatedShadowCostOnChoice(t *testing.T) {
	f := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(LabelEasy, 0.99)}}
	haiku := classifierStub(t, LabelModerate)
	r := NewJevRouter(f, NewClassifier(haiku, "S"), "S", 1)
	d, err := r.Route(context.Background(), "Q")
	if err != nil {
		t.Fatal(err)
	}
	if d.ShadowCostUSD != 0.0015 || d.Cost != d.Gate.PrimaryCost {
		t.Errorf("shadow cost %v cost %v", d.ShadowCostUSD, d.Cost)
	}
	fake := &tierAgent{results: allGood()}
	fj := &fakeJev{answers: []map[string]jev.Answer{choiceAnswer(VerdictAccept, 0.99)}}
	stub := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictReject)}, cost: 0.002}
	v := NewJevGatedVerifier(fj, NewVerifier(stub, llm.TierCheap, "SEM", nil), "SEM", 1)
	o, err := NewCascade(NameCascadeVerifyJev, fake, v).Solve(context.Background(), "q")
	if err != nil {
		t.Fatal(err)
	}
	if o.ShadowCost() != 0.002 || o.OverheadCost() != o.Attempts[0].VerifyGate.PrimaryCost {
		t.Errorf("cascade shadow %v overhead %v", o.ShadowCost(), o.OverheadCost())
	}
}
