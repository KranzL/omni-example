package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/llm"
)

const (
	ToolJudge     = "record_judgment"
	ToolSupervise = "review_judgment"

	VerdictPass = "pass"
	VerdictFail = "fail"

	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"

	DecisionAgree    = "agree"
	DecisionOverturn = "overturn"
)

const JudgeInstructions = `You grade one analytics answer against an expert rubric. You never answer the question yourself and you never write SQL. You read the question, the pinned correct answer, the failure criteria, the close-but-wrong examples, the SQL the agent submitted, and the agent's answer, then you call record_judgment exactly once. Always respond with that tool call and nothing else.

Pass when the answer matches the correct answer under the rubric: numbers within 1 percent relative tolerance, strings equal ignoring case and a trailing parenthetical, lists and tables matching under the same value rules, formatting differences ignored. Fail when any failure criterion holds or the answer matches a close-but-wrong example.

Confidence is high when the answer clearly matches or clearly violates the rubric, medium when rounding or formatting needs judgment, and low when the rubric does not settle the case. The reason is one sentence naming what decided the verdict.`

const SupervisorInstructions = `You supervise the grading of one analytics answer. You receive the question, the pinned correct answer, the expert rubric, the SQL the agent submitted, the agent's answer, and the first judge's verdict with its confidence and reason. You never answer the question yourself. You call review_judgment exactly once. Always respond with that tool call and nothing else.

Agree when the first judge applied the rubric correctly. Overturn only when the judge clearly misread the rubric or the answer: a correct answer marked fail, or a wrong answer marked pass. When the case is genuinely ambiguous, agree. The reason is one sentence naming what decided the review.`

type Judgment struct {
	Verdict    string `json:"verdict"`
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
}

type Supervision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func JudgeTools() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{OfTool: &anthropic.ToolParam{
			Name:        ToolJudge,
			Description: anthropic.String("Record the pass or fail verdict for the agent's answer. Call exactly once."),
			Strict:      anthropic.Bool(true),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"verdict": map[string]any{
						"type": "string",
						"enum": []string{VerdictPass, VerdictFail},
					},
					"confidence": map[string]any{
						"type": "string",
						"enum": []string{ConfidenceHigh, ConfidenceMedium, ConfidenceLow},
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One sentence naming what decided the verdict.",
					},
				},
				Required:    []string{"verdict", "confidence", "reason"},
				ExtraFields: map[string]any{"additionalProperties": false},
			},
		}},
	}
}

func SupervisorTools() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{OfTool: &anthropic.ToolParam{
			Name:        ToolSupervise,
			Description: anthropic.String("Agree with or overturn the first judge's verdict. Call exactly once."),
			Strict:      anthropic.Bool(true),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"decision": map[string]any{
						"type": "string",
						"enum": []string{DecisionAgree, DecisionOverturn},
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One sentence naming what decided the review.",
					},
				},
				Required:    []string{"decision", "reason"},
				ExtraFields: map[string]any{"additionalProperties": false},
			},
		}},
	}
}

func ParseJudgment(msg *anthropic.Message) (Judgment, error) {
	if msg == nil {
		return Judgment{}, fmt.Errorf("judge: empty response")
	}
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}
		tu := block.AsToolUse()
		if tu.Name != ToolJudge {
			continue
		}
		var j Judgment
		if err := json.Unmarshal(tu.Input, &j); err != nil {
			return Judgment{}, fmt.Errorf("judge: bad tool input: %w", err)
		}
		j.Verdict = strings.ToLower(strings.TrimSpace(j.Verdict))
		j.Confidence = strings.ToLower(strings.TrimSpace(j.Confidence))
		j.Reason = strings.TrimSpace(j.Reason)
		if j.Verdict != VerdictPass && j.Verdict != VerdictFail {
			return Judgment{}, fmt.Errorf("judge: unknown verdict %q", j.Verdict)
		}
		if j.Confidence != ConfidenceHigh && j.Confidence != ConfidenceMedium && j.Confidence != ConfidenceLow {
			return Judgment{}, fmt.Errorf("judge: unknown confidence %q", j.Confidence)
		}
		return j, nil
	}
	return Judgment{}, fmt.Errorf("judge: no %s call, stop_reason=%s", ToolJudge, msg.StopReason)
}

func ParseSupervision(msg *anthropic.Message) (Supervision, error) {
	if msg == nil {
		return Supervision{}, fmt.Errorf("supervisor: empty response")
	}
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}
		tu := block.AsToolUse()
		if tu.Name != ToolSupervise {
			continue
		}
		var s Supervision
		if err := json.Unmarshal(tu.Input, &s); err != nil {
			return Supervision{}, fmt.Errorf("supervisor: bad tool input: %w", err)
		}
		s.Decision = strings.ToLower(strings.TrimSpace(s.Decision))
		s.Reason = strings.TrimSpace(s.Reason)
		if s.Decision != DecisionAgree && s.Decision != DecisionOverturn {
			return Supervision{}, fmt.Errorf("supervisor: unknown decision %q", s.Decision)
		}
		return s, nil
	}
	return Supervision{}, fmt.Errorf("supervisor: no %s call, stop_reason=%s", ToolSupervise, msg.StopReason)
}

func NeedsSupervisor(j Judgment, graderCorrect *bool) bool {
	if j.Verdict == VerdictFail {
		return true
	}
	if j.Verdict != VerdictPass {
		return false
	}
	return j.Confidence == ConfidenceLow || (graderCorrect != nil && !*graderCorrect)
}

type Input struct {
	QuestionID    string
	Question      string
	Rubric        Rubric
	Expected      any
	Answer        string
	SQL           string
	GraderCorrect *bool
}

func (in Input) JudgePrompt() string {
	sql := strings.TrimSpace(in.SQL)
	if sql == "" {
		sql = "(the agent submitted no SQL)"
	}
	return fmt.Sprintf("Question:\n%s\n\nPinned correct answer:\n%s\n\nRubric correct answer:\n%s\n\nFailure criteria:\n%s\n\nClose but wrong:\n%s\n\nSubmitted SQL:\n%s\n\nAgent's answer:\n%s",
		in.Question, FormatExpected(in.Expected), in.Rubric.CorrectAnswer, in.Rubric.FailureCriteria, in.Rubric.CloseButWrong, sql, strings.TrimSpace(in.Answer))
}

func (in Input) SupervisorPrompt(j Judgment) string {
	return fmt.Sprintf("%s\n\nFirst judge verdict: %s\nFirst judge confidence: %s\nFirst judge reason: %s",
		in.JudgePrompt(), j.Verdict, j.Confidence, j.Reason)
}

type Outcome struct {
	Judgment    Judgment
	Supervised  bool
	Supervision Supervision
	FinalPass   bool
	CostUSD     float64
	Tokens      llm.Usage
	LatencyMS   int64
	Records     []llm.CallRecord
}

type Judge struct {
	LLM            llm.Provider
	JudgeTier      llm.Tier
	SupervisorTier llm.Tier
}

func New(p llm.Provider) *Judge {
	return &Judge{LLM: p, JudgeTier: llm.TierCheap, SupervisorTier: llm.TierMid}
}

func (j *Judge) JudgeOne(ctx context.Context, in Input) (Outcome, error) {
	start := time.Now()
	var out Outcome
	var trace llm.Trace
	var judgment Judgment
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		var msg *anthropic.Message
		msg, _, err = j.LLM.Call(ctx, llm.Request{
			Tier:     j.JudgeTier,
			Purpose:  "bench-judge-" + in.QuestionID,
			System:   []anthropic.TextBlockParam{{Text: JudgeInstructions}},
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(in.JudgePrompt()))},
			Tools:    JudgeTools(),
		}, &trace)
		if err == nil {
			judgment, err = ParseJudgment(msg)
		}
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	out.Records = trace.Records()
	out.CostUSD = trace.TotalCost()
	out.Tokens = trace.TotalUsage()
	if err != nil {
		out.LatencyMS = time.Since(start).Milliseconds()
		return out, err
	}
	out.Judgment = judgment
	out.FinalPass = judgment.Verdict == VerdictPass
	if !NeedsSupervisor(judgment, in.GraderCorrect) {
		out.LatencyMS = time.Since(start).Milliseconds()
		return out, nil
	}
	var supervision Supervision
	for attempt := 0; attempt < 2; attempt++ {
		var msg *anthropic.Message
		msg, _, err = j.LLM.Call(ctx, llm.Request{
			Tier:     j.SupervisorTier,
			Purpose:  "bench-judge-supervisor-" + in.QuestionID,
			System:   []anthropic.TextBlockParam{{Text: SupervisorInstructions}},
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(in.SupervisorPrompt(judgment)))},
			Tools:    SupervisorTools(),
		}, &trace)
		if err == nil {
			supervision, err = ParseSupervision(msg)
		}
		if err == nil || ctx.Err() != nil {
			break
		}
	}
	out.Records = trace.Records()
	out.CostUSD = trace.TotalCost()
	out.Tokens = trace.TotalUsage()
	out.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		return out, err
	}
	out.Supervised = true
	out.Supervision = supervision
	if supervision.Decision == DecisionOverturn {
		out.FinalPass = !out.FinalPass
	}
	return out, nil
}
