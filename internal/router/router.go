package router

import (
	"context"
	"fmt"
	"time"

	"github.com/KranzL/omni-example/internal/llm"
)

const (
	NameAlwaysCheap = "always-cheap"
	NameAlwaysMid   = "always-mid"
	NameAlwaysTop   = "always-top"
	NameAlwaysMuse  = "always-muse"
	NameHeuristic   = "heuristic"
	NameHeuristicV2 = "heuristic-v2"
	NameClassifier  = "classifier"

	LabelEasy     = "easy"
	LabelModerate = "moderate"
	LabelHard     = "hard"
	LabelFixed    = "fixed"
)

type Decision struct {
	Tier          llm.Tier         `json:"tier"`
	Label         string           `json:"label"`
	Reason        string           `json:"reason"`
	Cost          float64          `json:"cost_usd"`
	ShadowCostUSD float64          `json:"shadow_cost_usd,omitempty"`
	Latency       time.Duration    `json:"latency_ns"`
	CallRecords   []llm.CallRecord `json:"call_records,omitempty"`
	Gate          *GateInfo        `json:"gate,omitempty"`
}

type Router interface {
	Name() string
	Route(ctx context.Context, question string) (Decision, error)
}

func Names() []string {
	return []string{NameAlwaysCheap, NameAlwaysMid, NameAlwaysTop, NameAlwaysMuse, NameHeuristic, NameHeuristicV2, NameClassifier, NameEmbedding, NameJevClassifier}
}

func ValidName(name string) error {
	all := append(Names(), CascadeNames()...)
	for _, n := range all {
		if n == name {
			return nil
		}
	}
	return fmt.Errorf("unknown router %q, want one of %v", name, all)
}

func CanonicalLabel(label string) (string, error) {
	switch label {
	case LabelEasy, "simple":
		return LabelEasy, nil
	case LabelModerate:
		return LabelModerate, nil
	case LabelHard, "challenging":
		return LabelHard, nil
	default:
		return "", fmt.Errorf("unknown label %q, want easy, moderate or hard", label)
	}
}

func TierForLabel(label string) (llm.Tier, error) {
	canonical, err := CanonicalLabel(label)
	if err != nil {
		return "", err
	}
	switch canonical {
	case LabelEasy:
		return llm.TierCheap, nil
	case LabelModerate:
		return llm.TierMid, nil
	case LabelHard:
		return llm.TierTop, nil
	default:
		return "", fmt.Errorf("unknown label %q, want easy, moderate or hard", label)
	}
}

type Fixed struct {
	Tier llm.Tier
}

func (f Fixed) Name() string {
	return "always-" + string(f.Tier)
}

func (f Fixed) Route(ctx context.Context, question string) (Decision, error) {
	return Decision{Tier: f.Tier, Label: LabelFixed, Reason: f.Name()}, nil
}

func Baseline(name string) (Fixed, bool) {
	switch name {
	case NameAlwaysCheap:
		return Fixed{Tier: llm.TierCheap}, true
	case NameAlwaysMid:
		return Fixed{Tier: llm.TierMid}, true
	case NameAlwaysTop:
		return Fixed{Tier: llm.TierTop}, true
	case NameAlwaysMuse:
		return Fixed{Tier: llm.TierCheap}, true
	default:
		return Fixed{}, false
	}
}
