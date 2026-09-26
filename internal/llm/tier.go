package llm

import (
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

type Tier string

const (
	TierCheap Tier = "cheap"
	TierMid   Tier = "mid"
	TierTop   Tier = "top"
)

var Tiers = []Tier{TierCheap, TierMid, TierTop}

func ParseTier(s string) (Tier, error) {
	for _, t := range Tiers {
		if string(t) == s {
			return t, nil
		}
	}
	return "", fmt.Errorf("unknown tier %q", s)
}

const (
	ModelHaiku45 = "claude-haiku-4-5"
	ModelSonnet5 = "claude-sonnet-5"
	ModelOpus55  = "claude-opus-5-5"
)

type ThinkingMode string

const (
	ThinkingOff      ThinkingMode = "off"
	ThinkingAdaptive ThinkingMode = "adaptive"
	ThinkingBudget   ThinkingMode = "budget"
)

type Settings struct {
	Model          string
	MaxTokens      int64
	Thinking       ThinkingMode
	ThinkingBudget int64
	Effort         anthropic.OutputConfigEffort
}

func DefaultSettings() map[Tier]Settings {
	return map[Tier]Settings{
		TierCheap: {
			Model:     ModelHaiku45,
			MaxTokens: 4096,
			Thinking:  ThinkingOff,
		},
		TierMid: {
			Model:     ModelSonnet5,
			MaxTokens: 16000,
			Thinking:  ThinkingAdaptive,
			Effort:    anthropic.OutputConfigEffortMedium,
		},
		TierTop: {
			Model:     ModelOpus55,
			MaxTokens: 16000,
			Thinking:  ThinkingAdaptive,
			Effort:    anthropic.OutputConfigEffortMedium,
		},
	}
}

func (s Settings) Validate() error {
	if s.Model == "" {
		return fmt.Errorf("settings: empty model")
	}
	if s.MaxTokens <= 0 {
		return fmt.Errorf("settings %s: max_tokens must be positive", s.Model)
	}
	model := NormalizeModel(s.Model)
	switch s.Thinking {
	case ThinkingOff, ThinkingAdaptive:
	case ThinkingBudget:
		if s.ThinkingBudget < 1024 || s.ThinkingBudget >= s.MaxTokens {
			return fmt.Errorf("settings %s: thinking budget must be at least 1024 and below max_tokens", s.Model)
		}
	default:
		return fmt.Errorf("settings %s: unknown thinking mode %q", s.Model, s.Thinking)
	}
	if model == ModelHaiku45 {
		if s.Thinking == ThinkingAdaptive {
			return fmt.Errorf("settings %s: adaptive thinking is not supported, use a budget", s.Model)
		}
		if s.Effort != "" {
			return fmt.Errorf("settings %s: effort is not supported", s.Model)
		}
	} else if s.Thinking == ThinkingBudget {
		return fmt.Errorf("settings %s: budget thinking is not supported, use adaptive", s.Model)
	}
	if model == ModelOpus55 && s.Thinking == ThinkingOff {
		return fmt.Errorf("settings %s: thinking cannot be disabled, lower effort instead", s.Model)
	}
	return nil
}

func (s Settings) Apply(p *anthropic.MessageNewParams) {
	p.Model = anthropic.Model(s.Model)
	p.MaxTokens = s.MaxTokens
	switch s.Thinking {
	case ThinkingOff:
		p.Thinking = anthropic.ThinkingConfigParamUnion{OfDisabled: &anthropic.ThinkingConfigDisabledParam{}}
	case ThinkingAdaptive:
		p.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}
	case ThinkingBudget:
		p.Thinking = anthropic.ThinkingConfigParamOfEnabled(s.ThinkingBudget)
	}
	if s.Effort != "" {
		p.OutputConfig.Effort = s.Effort
	}
	if len(p.Tools) > 0 {
		p.ToolChoice = anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
	}
}
