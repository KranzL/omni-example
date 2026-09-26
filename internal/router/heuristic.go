package router

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/KranzL/omni-example/internal/semantic"
)

var (
	AggregationWords = []string{"total", "sum", "average", "avg", "mean", "median", "percentile", "rate", "share", "ratio", "per", "margin", "proportion", "percentage"}
	TimeWords        = []string{"month", "months", "monthly", "year", "years", "yearly", "week", "weekly", "quarter", "day", "days", "hours", "growth", "trend", "cohort", "cohorts", "acquisition", "acquired", "retention", "over time"}
	RankingWords     = []string{"top", "highest", "lowest", "most", "least", "largest", "smallest", "best", "worst", "versus", "vs", "compare", "rank", "difference"}
	ReasoningWords   = []string{"why", "optimize", "predict", "recommend", "forecast", "explain", "should", "improve", "estimate", "drivers"}
	QualifierWords   = []string{"at least", "within", "among", "each", "first", "second", "third", "repeat", "after", "before", "excluding", "subtracting", "between"}
)

type HeuristicParams struct {
	PerWord       float64 `json:"per_word"`
	SchemaMention float64 `json:"schema_mention"`
	Aggregation   float64 `json:"aggregation"`
	Time          float64 `json:"time"`
	Ranking       float64 `json:"ranking"`
	Reasoning     float64 `json:"reasoning"`
	Qualifier     float64 `json:"qualifier"`
	MidThreshold  float64 `json:"mid_threshold"`
	TopThreshold  float64 `json:"top_threshold"`
}

func DefaultHeuristicParams() HeuristicParams {
	return HeuristicParams{
		PerWord:       0.1,
		SchemaMention: 0.3,
		Aggregation:   0.5,
		Time:          0.5,
		Ranking:       0.5,
		Reasoning:     2.0,
		Qualifier:     0.75,
		MidThreshold:  3.0,
		TopThreshold:  5.0,
	}
}

func HeuristicV2Params() HeuristicParams {
	p := DefaultHeuristicParams()
	p.Aggregation = 0.7
	return p
}

type Features struct {
	Words          int `json:"words"`
	SchemaMentions int `json:"schema_mentions"`
	Aggregation    int `json:"aggregation"`
	Time           int `json:"time"`
	Ranking        int `json:"ranking"`
	Reasoning      int `json:"reasoning"`
	Qualifier      int `json:"qualifier"`
}

func (p HeuristicParams) Score(f Features) float64 {
	return p.PerWord*float64(f.Words) +
		p.SchemaMention*float64(f.SchemaMentions) +
		p.Aggregation*float64(f.Aggregation) +
		p.Time*float64(f.Time) +
		p.Ranking*float64(f.Ranking) +
		p.Reasoning*float64(f.Reasoning) +
		p.Qualifier*float64(f.Qualifier)
}

func (p HeuristicParams) Label(score float64) string {
	switch {
	case score >= p.TopThreshold:
		return LabelHard
	case score >= p.MidThreshold:
		return LabelModerate
	default:
		return LabelEasy
	}
}

type Heuristic struct {
	Label       string
	Params      HeuristicParams
	SchemaTerms []string
}

func NewHeuristic(sem *semantic.Semantic, params HeuristicParams) *Heuristic {
	return &Heuristic{Label: NameHeuristic, Params: params, SchemaTerms: SchemaTerms(sem)}
}

func NewNamedHeuristic(name string, sem *semantic.Semantic, params HeuristicParams) *Heuristic {
	return &Heuristic{Label: name, Params: params, SchemaTerms: SchemaTerms(sem)}
}

func (h *Heuristic) Name() string {
	return h.Label
}

func (h *Heuristic) Route(ctx context.Context, question string) (Decision, error) {
	start := time.Now()
	f := h.Features(question)
	score := h.Params.Score(f)
	label := h.Params.Label(score)
	tier, err := TierForLabel(label)
	if err != nil {
		return Decision{}, err
	}
	reason := fmt.Sprintf("score=%.2f words=%d schema=%d agg=%d time=%d rank=%d reason=%d qual=%d",
		score, f.Words, f.SchemaMentions, f.Aggregation, f.Time, f.Ranking, f.Reasoning, f.Qualifier)
	return Decision{Tier: tier, Label: label, Reason: reason, Latency: time.Since(start)}, nil
}

func (h *Heuristic) Features(question string) Features {
	norm := normalize(question)
	schemaText := norm
	mentions := 0
	for _, term := range h.SchemaTerms {
		needle := " " + term + " "
		if strings.Contains(schemaText, needle) {
			mentions++
			schemaText = strings.ReplaceAll(schemaText, needle, " | ")
		}
	}
	return Features{
		Words:          len(strings.Fields(norm)),
		SchemaMentions: mentions,
		Aggregation:    countTerms(norm, AggregationWords),
		Time:           countTerms(norm, TimeWords),
		Ranking:        countTerms(norm, RankingWords),
		Reasoning:      countTerms(norm, ReasoningWords),
		Qualifier:      countTerms(norm, QualifierWords),
	}
}

var genericColumns = map[string]bool{"id": true, "name": true}

func SchemaTerms(sem *semantic.Semantic) []string {
	seen := map[string]bool{}
	add := func(raw string) {
		t := strings.TrimSpace(normalize(raw))
		if t == "" || genericColumns[t] {
			return
		}
		seen[t] = true
	}
	if sem != nil {
		for _, t := range sem.Tables {
			add(t.Name)
			add(strings.TrimSuffix(t.Name, "s"))
			for _, c := range t.Columns {
				add(c.Name)
			}
		}
		for _, m := range sem.Metrics {
			add(m.Name)
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) > len(out[j])
		}
		return out[i] < out[j]
	})
	return out
}

func normalize(s string) string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return " " + strings.Join(fields, " ") + " "
}

func countTerms(norm string, terms []string) int {
	n := 0
	for _, t := range terms {
		if strings.Contains(norm, " "+t+" ") {
			n++
		}
	}
	return n
}
