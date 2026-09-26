package router

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
	ToolClassify            = "classify_difficulty"
	ClassifierFallbackLabel = LabelModerate
)

const ClassifierInstructions = classifierHead + "an ecommerce Postgres database" + classifierBody

const classifierHead = `You route analytics questions about `

const classifierBody = ` to one of three model tiers. You never answer the question and you never write SQL. You read the question, judge how much analytical work a SQL agent needs to answer it correctly, and call classify_difficulty exactly once. Always respond with that tool call and nothing else.

Difficulty rubric
- easy: one table or one obvious join, a direct lookup or a single count, sum or list with a simple filter such as a status value or a date window. The query is short and there is one reasonable reading of the question.
- moderate: a standard metric from the semantic layer, a GROUP BY over one dimension, a top N ranking, or a join across two or three tables with the join path spelled out in the semantic layer. One aggregation step; no per-entity intermediate results.
- hard: more than one aggregation step, where per-user, per-order or per-period values must be computed first and then aggregated again. Signals: first, second or repeat orders per user; cohorts or acquisition months; windows such as within N days after an event; medians or percentiles of derived values; month-over-month changes; ratios between two derived quantities; conditions that apply to groups (at least N of something); open-ended requests to explain, predict, recommend or optimize.

When a question sits between two levels, pick the higher one. Mistakes that send a hard question to a small model cost more than the savings on easy ones.

Labelled examples (not from the benchmark)
1. "What is the retail price of the product with id 2500?" -> easy: single-row lookup on products.
2. "How many users have country Brasil?" -> easy: one count with one filter.
3. "How many order items were returned in February 2024?" -> easy: one count on order_items with a status and date filter.
4. "What was the gross revenue by user country in calendar 2023?" -> moderate: a standard metric grouped by one dimension through the users join.
5. "Which 3 distribution centers shipped the most units in 2024?" -> moderate: a top N ranking across order_items, inventory_items and distribution_centers.
6. "What was the average sale price by product department in the second quarter of 2024?" -> moderate: one aggregation grouped by one dimension.
7. "What share of users whose first non-cancelled order was in Q1 2024 placed a second order within 60 days?" -> hard: per-user first and second orders, then a share over users.
8. "What was the median number of days from user creation to first Purchase event, by traffic source, in 2024?" -> hard: per-user first event, then a median per group.
9. "Which brand lost the most net revenue share between the first and second half of 2024?" -> hard: two derived shares per brand compared across periods.
10. "Recommend which product category to discount to raise the repeat purchase rate, based on 2023 cohorts." -> hard: open-ended analysis over cohorts.

The semantic layer the SQL agent works from follows.`

type Classification struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

type Classifier struct {
	LLM    llm.Provider
	Tier   llm.Tier
	System []anthropic.TextBlockParam
	Tools  []anthropic.ToolUnionParam
}

func NewClassifier(p llm.Provider, semanticText string) *Classifier {
	return NewClassifierWith(p, semanticText, PromptOptions{})
}

func NewClassifierWith(p llm.Provider, semanticText string, opts PromptOptions) *Classifier {
	return &Classifier{
		LLM:    p,
		Tier:   llm.TierCheap,
		System: ClassifierSystemWith(semanticText, opts),
		Tools:  ClassifierTools(),
	}
}

func ClassifierInstructionsFor(d Domain) string {
	return classifierHead + d.indefinite() + classifierBody
}

func ClassifierSystem(semanticText string) []anthropic.TextBlockParam {
	return ClassifierSystemWith(semanticText, PromptOptions{})
}

func ClassifierSystemWith(semanticText string, opts PromptOptions) []anthropic.TextBlockParam {
	return systemBlocks(ClassifierInstructionsFor(opts.Domain), semanticText, opts.NoCache)
}

func ClassifierTools() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		{OfTool: &anthropic.ToolParam{
			Name:        ToolClassify,
			Description: anthropic.String("Record the difficulty label for the question. Call exactly once."),
			Strict:      anthropic.Bool(true),
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: map[string]any{
					"label": map[string]any{
						"type": "string",
						"enum": DifficultyLabels(),
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "One sentence naming the feature of the question that decided the label.",
					},
				},
				Required:    []string{"label", "reason"},
				ExtraFields: map[string]any{"additionalProperties": false},
			},
		}},
	}
}

func (c *Classifier) Name() string {
	return NameClassifier
}

func (c *Classifier) Point() string {
	return PointDifficulty
}

func (c *Classifier) Labels() []string {
	return DifficultyLabels()
}

func (c *Classifier) Decide(ctx context.Context, in DecisionInput) (Choice, error) {
	start := time.Now()
	var trace llm.Trace
	var cl Classification
	var err, parseErr error
	for attempt := 0; attempt < 2; attempt++ {
		var msg *anthropic.Message
		msg, _, err = c.LLM.Call(ctx, llm.Request{
			Tier:     c.Tier,
			Purpose:  "route-classifier",
			System:   c.System,
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Question: " + in.Question))},
			Tools:    c.Tools,
		}, &trace)
		parseErr = nil
		if err == nil {
			cl, parseErr = ParseClassification(msg)
		}
		if (err == nil && parseErr == nil) || ctx.Err() != nil {
			break
		}
	}
	ch := Choice{Cost: trace.TotalCost(), Latency: time.Since(start), CallRecords: trace.Records()}
	if err != nil {
		return ch, err
	}
	if parseErr != nil {
		ch.Label = ClassifierFallbackLabel
		ch.ParseError = parseErr.Error()
		ch.Reason = fmt.Sprintf("unparseable classifier reply, fallback %s: %s", ClassifierFallbackLabel, parseErr)
		return ch, nil
	}
	ch.Label = cl.Label
	ch.Reason = cl.Reason
	return ch, nil
}

func (c *Classifier) Route(ctx context.Context, question string) (Decision, error) {
	ch, err := c.Decide(ctx, DecisionInput{Question: question})
	d := Decision{Cost: ch.Cost, Latency: ch.Latency, CallRecords: ch.CallRecords}
	if err != nil {
		return d, err
	}
	tier, err := TierForLabel(ch.Label)
	if err != nil {
		return d, err
	}
	d.Tier = tier
	d.Label = ch.Label
	d.Reason = ch.Reason
	return d, nil
}

func ParseClassification(msg *anthropic.Message) (Classification, error) {
	if msg == nil {
		return Classification{}, fmt.Errorf("classifier: empty response")
	}
	for _, block := range msg.Content {
		if block.Type != "tool_use" {
			continue
		}
		tu := block.AsToolUse()
		if tu.Name != ToolClassify {
			continue
		}
		var cl Classification
		if err := json.Unmarshal(tu.Input, &cl); err != nil {
			return Classification{}, fmt.Errorf("classifier: bad tool input: %w", err)
		}
		cl.Label = strings.ToLower(strings.TrimSpace(cl.Label))
		cl.Reason = strings.TrimSpace(cl.Reason)
		if _, err := TierForLabel(cl.Label); err != nil {
			return Classification{}, fmt.Errorf("classifier: %w", err)
		}
		return cl, nil
	}
	return Classification{}, fmt.Errorf("classifier: no %s call, stop_reason=%s", ToolClassify, msg.StopReason)
}
