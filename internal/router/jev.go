package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/jev"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	NameJevClassifier    = "jev-classifier"
	NameCascadeVerifyJev = "cascade-verify-jev"

	JevQuestionDifficulty = "difficulty"
	JevQuestionDepth      = "depth"
	JevQuestionVerdict    = "verdict"

	PurposeJevRoute  = "route-jev"
	PurposeJevVerify = "verifier-jev"
)

var JevDifficultyThresholds = map[string]float64{
	LabelEasy:     0.85,
	LabelModerate: 0.70,
	LabelHard:     0.50,
}

var JevVerdictThresholds = map[string]float64{
	VerdictAccept: 0.90,
	VerdictReject: 0.80,
}

type JevAsker interface {
	Ask(ctx context.Context, purpose string, state any, questions map[string]jev.Question, trace *llm.Trace) (jev.Response, llm.CallRecord, error)
}

const jevDifficultyInstructions = jevDifficultyHead + "the ecommerce Postgres database" + jevDifficultyBody

const jevDifficultyHead = "A SQL agent will answer `question` by querying "

const jevDifficultyBody = " described in `semantic_layer`. How much analytical work does the agent need to answer it correctly? When the question sits between two levels, pick the higher one."

var jevDifficultyCriteria = map[string]any{
	LabelEasy:     "One table or one obvious join; a direct lookup, or a single count, sum or list with a simple filter such as a status value or a date window. The query is short and there is one reasonable reading of the question.",
	LabelModerate: "A standard metric from the semantic layer, a GROUP BY over one dimension, a top N ranking, or a join across two or three tables with the join path spelled out in the semantic layer. One aggregation step and no per-entity intermediate results.",
	LabelHard:     "More than one aggregation step: per-user, per-order or per-period values are computed first and then aggregated again. Signals: first, second or repeat orders per user; cohorts or acquisition months; windows such as within N days after an event; medians or percentiles of derived values; month-over-month changes; ratios between two derived quantities; conditions on groups such as at least N of something; open-ended requests to explain, predict, recommend or optimize.",
}

const jevDepthInstructions = "How many aggregation steps does the SQL for `question` need?"

var jevDepthLevels = []any{
	"None or one: a lookup, or one aggregate with filters.",
	"One aggregation grouped by a dimension or ranked, possibly across a join.",
	"Two or more: per-entity or per-period values are computed and then aggregated again.",
}

func JevDifficultyQuestions() map[string]jev.Question {
	return JevDifficultyQuestionsFor(Domain{})
}

func JevDifficultyQuestionsFor(d Domain) map[string]jev.Question {
	return map[string]jev.Question{
		JevQuestionDifficulty: jev.Choice(jevDifficultyHead+d.definite()+jevDifficultyBody, jevDifficultyCriteria),
		JevQuestionDepth:      jev.Score(jevDepthInstructions, jevDepthLevels),
	}
}

type JevClassifier struct {
	Client       JevAsker
	SemanticText string
	Domain       Domain
}

func NewJevClassifier(c JevAsker, semanticText string) *JevClassifier {
	return &JevClassifier{Client: c, SemanticText: semanticText}
}

func (j *JevClassifier) Name() string {
	return "jev-difficulty"
}

func (j *JevClassifier) Point() string {
	return PointDifficulty
}

func (j *JevClassifier) Labels() []string {
	return DifficultyLabels()
}

func (j *JevClassifier) Decide(ctx context.Context, in DecisionInput) (Choice, error) {
	state := map[string]any{
		"semantic_layer": j.SemanticText,
		"question":       in.Question,
	}
	return jevDecide(ctx, j.Client, PurposeJevRoute, state, JevDifficultyQuestionsFor(j.Domain), JevQuestionDifficulty, func(resp jev.Response) string {
		d, ok := resp.Answers[JevQuestionDepth]
		if !ok {
			return ""
		}
		return fmt.Sprintf("; depth %.2f conf %.2f", d.Score, d.Confidence)
	})
}

const jevVerdictInstructions = "A SQL analyst agent answered `question` about the database described in `semantic_layer`. It submitted `sql`, which returned `rows`, and gave `answer`. Should the answer be accepted? Reject only for a concrete defect."

var jevVerdictCriteria = map[string]any{
	VerdictAccept: "The SQL is a reasonable reading of the question under the semantic layer's conventions and the answer matches the rows. Formatting, rounding the question did not ask for, column aliases, or a different but equivalent query are not defects.",
	VerdictReject: "A concrete defect: the SQL uses a wrong table or join path, a missing or wrong filter (status, date window, category, country), a date window that does not match the question's period, a metric that departs from its definition, double counting through a fan-out join, or a missing per-entity step; or the answer does not follow from the rows (wrong value, wrong row, wrong order, missing items); or the rows are empty or an error when the question implies a result.",
}

func JevVerdictQuestions() map[string]jev.Question {
	return map[string]jev.Question{
		JevQuestionVerdict: jev.Choice(jevVerdictInstructions, jevVerdictCriteria),
	}
}

type JevVerifier struct {
	Client       JevAsker
	SemanticText string
}

func NewJevVerifier(c JevAsker, semanticText string) *JevVerifier {
	return &JevVerifier{Client: c, SemanticText: semanticText}
}

func (j *JevVerifier) Name() string {
	return "jev-verdict"
}

func (j *JevVerifier) Point() string {
	return PointVerdict
}

func (j *JevVerifier) Labels() []string {
	return VerdictLabels()
}

func (j *JevVerifier) Decide(ctx context.Context, in DecisionInput) (Choice, error) {
	state := map[string]any{
		"semantic_layer": j.SemanticText,
		"question":       in.Question,
		"sql":            in.SQL,
		"rows":           in.Rows,
		"answer":         in.Answer,
	}
	return jevDecide(ctx, j.Client, PurposeJevVerify, state, JevVerdictQuestions(), JevQuestionVerdict, nil)
}

func jevDecide(ctx context.Context, c JevAsker, purpose string, state any, qs map[string]jev.Question, id string, extra func(jev.Response) string) (Choice, error) {
	start := time.Now()
	var trace llm.Trace
	resp, _, err := c.Ask(ctx, purpose, state, qs, &trace)
	ch := Choice{Cost: trace.TotalCost(), Latency: time.Since(start), CallRecords: trace.Records()}
	if err != nil {
		return ch, err
	}
	a, err := resp.Answer(id)
	if err != nil {
		return ch, err
	}
	if a.Type != jev.TypeChoice {
		return ch, fmt.Errorf("jev: answer %q has type %q, want choice", id, a.Type)
	}
	ch.Label = strings.ToLower(strings.TrimSpace(a.Choice))
	ch.Score = a.Confidence
	ch.Scored = true
	ch.Probs = a.Probabilities
	ch.Reason = fmt.Sprintf("p=%.2f conf %.2f", a.Probabilities[a.Choice], a.Confidence)
	if extra != nil {
		ch.Reason += extra(resp)
	}
	return ch, nil
}

func NewJevRouter(c JevAsker, fallback Decider, semanticText string, shadowRate float64) *DecisionRouter {
	return NewJevRouterFor(c, fallback, semanticText, shadowRate, Domain{})
}

func NewJevRouterFor(c JevAsker, fallback Decider, semanticText string, shadowRate float64, d Domain) *DecisionRouter {
	primary := NewJevClassifier(c, semanticText)
	primary.Domain = d
	return &DecisionRouter{
		Label: NameJevClassifier,
		Decider: &Gated{
			Label:            NameJevClassifier,
			Primary:          primary,
			Fallback:         fallback,
			Thresholds:       JevDifficultyThresholds,
			DefaultThreshold: 1,
			ShadowRate:       shadowRate,
		},
	}
}

func NewJevGatedVerifier(c JevAsker, fallback *Verifier, semanticText string, shadowRate float64) *Verifier {
	v := *fallback
	v.Decider = &Gated{
		Label:            "verifier-jev",
		Primary:          NewJevVerifier(c, semanticText),
		Fallback:         fallback,
		Thresholds:       JevVerdictThresholds,
		DefaultThreshold: 1,
		ShadowRate:       shadowRate,
	}
	return &v
}
