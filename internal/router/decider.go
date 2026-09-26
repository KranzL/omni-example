package router

import (
	"context"
	"time"

	"github.com/KranzL/omni-example/internal/llm"
)

const (
	PointDifficulty = "difficulty"
	PointVerdict    = "verdict"
)

type DecisionInput struct {
	Question string
	SQL      string
	Rows     string
	Answer   string
}

type Choice struct {
	Label         string
	Reason        string
	Score         float64
	Scored        bool
	Probs         map[string]float64
	ParseError    string
	Gate          *GateInfo
	Cost          float64
	ShadowCostUSD float64
	Latency       time.Duration
	CallRecords   []llm.CallRecord
}

type Decider interface {
	Name() string
	Point() string
	Labels() []string
	Decide(ctx context.Context, in DecisionInput) (Choice, error)
}

func DifficultyLabels() []string {
	return []string{LabelEasy, LabelModerate, LabelHard}
}

func VerdictLabels() []string {
	return []string{VerdictAccept, VerdictReject}
}
