package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	VeniceBaseURL = "https://api.venice.ai/api/v1"

	VeniceQwen359B        = "qwen3-5-9b"
	VeniceDeepseekV4Flash = "deepseek-v4-flash"
	VeniceQwen37Plus      = "qwen-3-7-plus"
	VeniceEmbedBgeM3      = "text-embedding-bge-m3"
)

var VenicePrices = map[string]Price{
	VeniceQwen359B:        {Input: 0.10, Output: 0.15, CacheRead: 0.10},
	VeniceDeepseekV4Flash: {Input: 0.138, Output: 0.275, CacheRead: 0.028},
	VeniceQwen37Plus:      {Input: 0.50, Output: 2.00, CacheWrite5m: 0.625, CacheRead: 0.05},
	VeniceEmbedBgeM3:      {Input: 0.15},
}

func VeniceDefaultSettings() map[Tier]OpenAISettings {
	return map[Tier]OpenAISettings{
		TierCheap: {Model: VeniceQwen359B, MaxTokens: 4096, ReasoningEffort: "none"},
		TierMid:   {Model: VeniceDeepseekV4Flash, MaxTokens: 16000, ReasoningEffort: "low"},
		TierTop:   {Model: VeniceQwen37Plus, MaxTokens: 16000},
	}
}

func VeniceSettings(models map[Tier]string) map[Tier]OpenAISettings {
	out := VeniceDefaultSettings()
	for tier, model := range models {
		if model == "" {
			continue
		}
		s := out[tier]
		if model != s.Model {
			s.Model = model
			s.ReasoningEffort = ""
			s.DisableThinking = tier == TierCheap
		}
		out[tier] = s
	}
	return out
}

func NewVeniceClient(apiKey string, models map[Tier]string) *OpenAIClient {
	return NewOpenAIClient(ProviderVenice, VeniceBaseURL, apiKey, VeniceSettings(models), map[string]any{
		"include_venice_system_prompt": false,
		"strip_thinking_response":      true,
		"enable_web_search":            "off",
	})
}

type VeniceModel struct {
	ID        string `json:"id"`
	ModelSpec struct {
		AvailableContextTokens int64                      `json:"availableContextTokens"`
		Pricing                map[string]json.RawMessage `json:"pricing"`
		Capabilities           struct {
			SupportsFunctionCalling bool     `json:"supportsFunctionCalling"`
			SupportsReasoning       bool     `json:"supportsReasoning"`
			SupportsReasoningEffort bool     `json:"supportsReasoningEffort"`
			ReasoningEffortOptions  []string `json:"reasoningEffortOptions"`
			DefaultReasoningEffort  string   `json:"defaultReasoningEffort"`
		} `json:"capabilities"`
	} `json:"model_spec"`
}

func (m VeniceModel) PriceUSD(key string) (float64, bool) {
	raw, ok := m.ModelSpec.Pricing[key]
	if !ok {
		return 0, false
	}
	var p struct {
		USD *float64 `json:"usd"`
	}
	if json.Unmarshal(raw, &p) != nil || p.USD == nil {
		return 0, false
	}
	return *p.USD, true
}

func FetchVeniceModels(ctx context.Context, baseURL, apiKey string) ([]VeniceModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models?type=text", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := (&http.Client{Timeout: time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models: status %d", resp.StatusCode)
	}
	var out struct {
		Data []VeniceModel `json:"data"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}
