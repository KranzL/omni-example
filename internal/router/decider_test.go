package router

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/llm"
)

var (
	_ Decider = (*Classifier)(nil)
	_ Decider = (*Verifier)(nil)
)

func TestDeciderAnswerSpaces(t *testing.T) {
	c := NewClassifier(&stubProvider{}, "S")
	v := NewVerifier(&stubProvider{}, llm.TierCheap, "S", nil)
	cases := []struct {
		d     Decider
		name  string
		point string
		enum  []string
		tools []anthropic.ToolUnionParam
	}{
		{c, NameClassifier, PointDifficulty, []string{"easy", "moderate", "hard"}, c.Tools},
		{v, "verifier-cheap", PointVerdict, []string{"accept", "reject"}, v.Tools},
	}
	for _, tc := range cases {
		if tc.d.Name() != tc.name || tc.d.Point() != tc.point || !reflect.DeepEqual(tc.d.Labels(), tc.enum) {
			t.Errorf("%s: name %q point %q labels %v", tc.name, tc.d.Name(), tc.d.Point(), tc.d.Labels())
		}
		props := tc.tools[0].OfTool.InputSchema.Properties.(map[string]any)
		var field string
		for k := range props {
			if k != "reason" {
				field = k
			}
		}
		if got := props[field].(map[string]any)["enum"]; !reflect.DeepEqual(got, tc.enum) {
			t.Errorf("%s: tool enum %v, labels %v", tc.name, got, tc.enum)
		}
	}
}

func TestClassifierDecideMatchesRoute(t *testing.T) {
	stub := &stubProvider{msg: toolMessage(t, ToolClassify, map[string]string{"label": "hard", "reason": "cohorts"}), cost: 0.0015}
	c := NewClassifier(stub, "S")
	ch, err := c.Decide(context.Background(), DecisionInput{Question: "Q"})
	if err != nil {
		t.Fatal(err)
	}
	decideReq := stub.req
	d, err := c.Route(context.Background(), "Q")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decideReq, stub.req) {
		t.Error("Decide and Route sent different requests")
	}
	if ch.Label != LabelHard || ch.Reason != "cohorts" || ch.Scored || ch.Cost != 0.0015 || len(ch.CallRecords) != 1 {
		t.Errorf("choice %+v", ch)
	}
	if d.Label != ch.Label || d.Reason != ch.Reason || d.Tier != llm.TierTop || d.Cost != ch.Cost {
		t.Errorf("route %+v from choice %+v", d, ch)
	}
}

func TestClassifierDecideErrorKeepsCost(t *testing.T) {
	stub := &stubProvider{err: errors.New("boom"), cost: 0.002}
	ch, err := NewClassifier(stub, "S").Decide(context.Background(), DecisionInput{Question: "Q"})
	if err == nil || ch.Label != "" || ch.Cost != 0.004 || len(ch.CallRecords) != 2 {
		t.Errorf("choice %+v err %v", ch, err)
	}
}

func TestClassifierDecideParseFailureFallsBack(t *testing.T) {
	bad := toolMessage(t, ToolClassify, map[string]string{"label": "expert", "reason": "x"})
	p := &seqProvider{msgs: []*anthropic.Message{bad, bad}, cost: 0.001}
	ch, err := NewClassifier(p, "S").Decide(context.Background(), DecisionInput{Question: "Q"})
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if p.calls != 2 || ch.Label != LabelModerate || ch.ParseError == "" || ch.Cost != 0.002 || len(ch.CallRecords) != 2 {
		t.Errorf("calls %d choice %+v", p.calls, ch)
	}
	good := toolMessage(t, ToolClassify, map[string]string{"label": "hard", "reason": "cohorts"})
	p = &seqProvider{msgs: []*anthropic.Message{bad, good}, cost: 0.001}
	ch, err = NewClassifier(p, "S").Decide(context.Background(), DecisionInput{Question: "Q"})
	if err != nil || ch.Label != LabelHard || ch.ParseError != "" || p.calls != 2 {
		t.Errorf("retry must use the second reply: calls %d choice %+v err %v", p.calls, ch, err)
	}
}

func TestGatedFallbackClassifierParseFailure(t *testing.T) {
	bad := toolMessage(t, ToolClassify, map[string]string{"label": "expert", "reason": "x"})
	g := &Gated{
		Primary:  NewClassifier(&stubProvider{err: errors.New("down")}, "S"),
		Fallback: NewClassifier(&seqProvider{msgs: []*anthropic.Message{bad, bad}, cost: 0.001}, "S"),
	}
	d, err := (&DecisionRouter{Label: "gated", Decider: g}).Route(context.Background(), "Q")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if d.Tier != llm.TierMid || d.Gate == nil || !d.Gate.FellBack || d.Gate.FallbackLabel != LabelModerate || d.Gate.FallbackReason != FallbackError {
		t.Errorf("decision %+v gate %+v", d, d.Gate)
	}
}

func TestVerifierDecidePrompt(t *testing.T) {
	p := &seqProvider{msgs: []*anthropic.Message{verdictMsg(t, VerdictReject)}, cost: 0.003}
	v := NewVerifier(p, llm.TierCheap, "S", nil)
	ch, err := v.Decide(context.Background(), DecisionInput{Question: "Q", SQL: "SELECT 1", Rows: "rows: 1", Answer: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if ch.Label != VerdictReject || ch.Cost != 0.003 || len(ch.CallRecords) != 1 {
		t.Errorf("choice %+v", ch)
	}
	want := "Question:\nQ\n\nSubmitted SQL:\nSELECT 1\n\nRows returned by the submitted SQL:\nrows: 1\n\nAgent's answer:\n1"
	if got := p.reqs[0].Messages[0].Content[0].OfText.Text; got != want {
		t.Errorf("prompt %q", got)
	}
}

func TestVerifierDecideRetriesOnceThenFails(t *testing.T) {
	bad := toolMessage(t, "other_tool", map[string]string{})
	p := &seqProvider{msgs: []*anthropic.Message{bad, bad, verdictMsg(t, VerdictAccept)}, cost: 0.001}
	ch, err := NewVerifier(p, llm.TierCheap, "S", nil).Decide(context.Background(), DecisionInput{Question: "Q"})
	if err == nil || !strings.Contains(err.Error(), ToolVerdict) {
		t.Fatalf("err %v", err)
	}
	if p.calls != 2 || ch.Label != "" || ch.Cost != 0.002 || len(ch.CallRecords) != 2 {
		t.Errorf("calls %d choice %+v", p.calls, ch)
	}
}
