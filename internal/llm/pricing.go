package llm

import (
	"fmt"
	"regexp"

	"github.com/anthropics/anthropic-sdk-go"
)

type Price struct {
	Input        float64
	Output       float64
	CacheWrite5m float64
	CacheWrite1h float64
	CacheRead    float64
}

var Prices = map[string]Price{
	ModelHaiku45: {Input: 1.00, Output: 5.00, CacheWrite5m: 1.25, CacheWrite1h: 2.00, CacheRead: 0.10},
	ModelSonnet5: {Input: 2.00, Output: 10.00, CacheWrite5m: 2.50, CacheWrite1h: 4.00, CacheRead: 0.20},
	ModelOpus55:  {Input: 4.00, Output: 20.00, CacheWrite5m: 5.00, CacheWrite1h: 8.00, CacheRead: 0.20},
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

func NormalizeModel(id string) string {
	return dateSuffix.ReplaceAllString(id, "")
}

func PriceFor(model string) (Price, bool) {
	if p, ok := Prices[NormalizeModel(model)]; ok {
		return p, true
	}
	if p, ok := VenicePrices[model]; ok {
		return p, true
	}
	p, ok := MusePrices[model]
	return p, ok
}

type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreation1hTokens    int64 `json:"cache_creation_1h_input_tokens,omitempty"`
}

func UsageFrom(u anthropic.Usage) Usage {
	return Usage{
		InputTokens:              u.InputTokens,
		OutputTokens:             u.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		CacheCreation1hTokens:    u.CacheCreation.Ephemeral1hInputTokens,
	}
}

func (u Usage) Add(o Usage) Usage {
	return Usage{
		InputTokens:              u.InputTokens + o.InputTokens,
		OutputTokens:             u.OutputTokens + o.OutputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + o.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + o.CacheReadInputTokens,
		CacheCreation1hTokens:    u.CacheCreation1hTokens + o.CacheCreation1hTokens,
	}
}

func (u Usage) PromptTokens() int64 {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

func (p Price) Cost(u Usage) float64 {
	write1h := min(u.CacheCreation1hTokens, u.CacheCreationInputTokens)
	write5m := u.CacheCreationInputTokens - write1h
	micro := float64(u.InputTokens)*p.Input +
		float64(u.OutputTokens)*p.Output +
		float64(write5m)*p.CacheWrite5m +
		float64(write1h)*p.CacheWrite1h +
		float64(u.CacheReadInputTokens)*p.CacheRead
	return micro / 1e6
}

func Cost(model string, u Usage) (float64, error) {
	p, ok := PriceFor(model)
	if !ok {
		return 0, fmt.Errorf("no price for model %q", model)
	}
	return p.Cost(u), nil
}
