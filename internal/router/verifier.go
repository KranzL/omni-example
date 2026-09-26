package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	ToolVerdict = "record_verdict"

	VerdictAccept = "accept"
	VerdictReject = "reject"
	VerdictError  = "error"

	VerifierMaxRows      = 100
	VerifierMaxRowsChars = 6000
)

const VerifierInstructions = verifierHead + "the ecommerce Postgres database" + verifierBody

const verifierHead = `You check the work of a SQL analyst agent that answered one question about `

const verifierBody = ` described in the next block. You receive the question, the SQL the agent submitted, the rows that SQL returned, and the agent's answer. You never write SQL and you never answer the question yourself. You decide whether the answer should be accepted, and you call record_verdict exactly once. Always respond with that tool call and nothing else.

Reject when any of these holds
- The SQL does not compute what the question asks: a wrong table or join path, a missing or wrong filter (status, date window, category, country), a date window that does not match the question's period, a metric that departs from its definition in the Metrics section, double counting through a fan-out join, or a missing per-entity step the question requires (first order per user, per-month values before a median, and similar).
- The answer does not follow from the rows: a value copied wrongly, the wrong row picked, a list in the wrong order, or items missing that the rows contain.
- The rows are empty or an error, and the question implies a non-empty result.

Accept when the SQL is a reasonable reading of the question under the semantic layer's conventions and the answer matches the rows. Do not reject for formatting, rounding the question did not ask for, column aliases, or a different but equivalent way to write the query. When you cannot find a concrete defect, accept.

The reason is one sentence. For a reject, name the defect.

The semantic layer the agent worked from follows.`

type Verdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

type VerifyResult struct {
	Verdict       Verdict
	Cost          float64
	ShadowCostUSD float64
	Latency       time.Duration
	CallRecords   []llm.CallRecord
	Gate          *GateInfo
}

type Verifier struct {
	LLM      llm.Provider
	Tier     llm.Tier
	System   []anthropic.TextBlockParam
	Tools    []anthropic.ToolUnionParam
	Querier  agent.Querier
	Decider  Decider
	Evidence string
}

func NewVerifier(p llm.Provider, tier llm.Tier, semanticText string, q agent.Querier) *Verifier {
	return NewVerifierWith(p, tier, semanticText, q, PromptOptions{})
}

func NewVerifierWith(p llm.Provider, tier llm.Tier, semanticText string, q agent.Querier, opts PromptOptions) *Verifier {
	return &Verifier{
		LLM:     p,
		Tier:    tier,
		System:  VerifierSystemWith(semanticText, opts),
		Tools:   VerifierTools(),
		Querier: q,
	}
}

func VerifierInstructionsFor(d Domain) string {
	return verifierHead + d.definite() + verifierBody
}

func VerifierSystem(semanticText string) []anthropic.TextBlockParam {
	return VerifierSystemWith(semanticText, PromptOptions{})
}

func VerifierSystemWith(semanticText string, opts PromptOptions) []anthropic.TextBlockParam {
	return systemBlocks(VerifierInstructionsFor(opts.Domain), semanticText, opts.NoCache)
}

func VerifierTools() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{OfTool: &anthropic.ToolParam{
			Name:        ToolVerdict,
			Description: anthropic.String("Record whether the agent's answer is accepted or rejected. Call exactly once."),
			Strict:      anthropic.Bool(true),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"verdict": map[string]any{
						"type": "string",
						"enum": VerdictLabels(),
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One sentence. For a reject, the concrete defect.",
					},
				},
				Required:    []string{"verdict", "reason"},
				ExtraFields: map[string]any{"additionalProperties": false},
			},
		}},
	}
}

func (v *Verifier) Name() string {
	return "verifier-" + string(v.Tier)
}

func (v *Verifier) Point() string {
	return PointVerdict
}

func (v *Verifier) Labels() []string {
	return VerdictLabels()
}

func (v *Verifier) Decide(ctx context.Context, in DecisionInput) (Choice, error) {
	start := time.Now()
	prompt := fmt.Sprintf("Question:\n%s\n\nSubmitted SQL:\n%s\n\nRows returned by the submitted SQL:\n%s\n\nAgent's answer:\n%s", in.Question, in.SQL, in.Rows, in.Answer)
	var trace llm.Trace
	var verdict Verdict
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var msg *anthropic.Message
		msg, _, err = v.LLM.Call(ctx, llm.Request{
			Tier:     v.Tier,
			Purpose:  "verifier",
			System:   v.System,
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
			Tools:    v.Tools,
		}, &trace)
		if err == nil {
			verdict, err = ParseVerdict(msg)
		}
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	return Choice{
		Label:       verdict.Verdict,
		Reason:      verdict.Reason,
		Cost:        trace.TotalCost(),
		Latency:     time.Since(start),
		CallRecords: trace.Records(),
	}, err
}

func (v *Verifier) Verify(ctx context.Context, question string, res agent.Result) (VerifyResult, error) {
	start := time.Now()
	var d Decider = v
	if v.Decider != nil {
		d = v.Decider
	}
	if strings.TrimSpace(v.Evidence) != "" {
		question += agent.EvidenceBlock(v.Evidence)
	}
	ch, err := d.Decide(ctx, DecisionInput{Question: question, SQL: res.SQL, Rows: v.rowsText(ctx, res), Answer: res.Answer})
	return VerifyResult{
		Verdict:       Verdict{Verdict: ch.Label, Reason: ch.Reason},
		Cost:          ch.Cost,
		ShadowCostUSD: ch.ShadowCostUSD,
		Latency:       time.Since(start),
		CallRecords:   ch.CallRecords,
		Gate:          ch.Gate,
	}, err
}

func (v *Verifier) rowsText(ctx context.Context, res agent.Result) string {
	if strings.TrimSpace(res.SQL) == "" {
		return "(the agent submitted no SQL)"
	}
	var text string
	if step, ok := SubmittedStep(res); ok {
		text = step.Output
		if step.Error != "" {
			text = "error: " + step.Error
		}
	} else if v.Querier != nil {
		r, err := v.Querier.Query(ctx, res.SQL, VerifierMaxRows)
		if err != nil {
			text = "error: " + err.Error()
		} else {
			text = agent.FormatResult(r)
		}
	} else {
		text = "(the submitted SQL was not run by the agent)"
	}
	if len(text) > VerifierMaxRowsChars {
		text = text[:VerifierMaxRowsChars] + "\n(truncated)"
	}
	return text
}

func ParseVerdict(msg *anthropic.Message) (Verdict, error) {
	if msg == nil {
		return Verdict{}, fmt.Errorf("verifier: empty response")
	}
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}
		tu := block.AsToolUse()
		if tu.Name != ToolVerdict {
			continue
		}
		var v Verdict
		if err := json.Unmarshal(tu.Input, &v); err != nil {
			return Verdict{}, fmt.Errorf("verifier: bad tool input: %w", err)
		}
		v.Verdict = strings.ToLower(strings.TrimSpace(v.Verdict))
		v.Reason = strings.TrimSpace(v.Reason)
		if v.Verdict != VerdictAccept && v.Verdict != VerdictReject {
			return Verdict{}, fmt.Errorf("verifier: unknown verdict %q", v.Verdict)
		}
		return v, nil
	}
	return Verdict{}, fmt.Errorf("verifier: no %s call, stop_reason=%s", ToolVerdict, msg.StopReason)
}

func NormalizeSQL(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	return strings.TrimSpace(strings.TrimRight(s, "; "))
}

func SubmittedStep(res agent.Result) (agent.SQLStep, bool) {
	want := NormalizeSQL(res.SQL)
	if want == "" {
		return agent.SQLStep{}, false
	}
	for i := len(res.Steps) - 1; i >= 0; i-- {
		if NormalizeSQL(res.Steps[i].SQL) == want {
			return res.Steps[i], true
		}
	}
	return agent.SQLStep{}, false
}
