package llm

import (
	"encoding/json"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

func requestJSON(t *testing.T, s Settings, withTools bool) map[string]any {
	t.Helper()
	p := anthropic.MessageNewParams{
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
	}
	if withTools {
		p.Tools = []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
			Name:        "run_sql",
			InputSchema: anthropic.ToolInputSchemaParam{Properties: map[string]any{}},
		}}}
	}
	s.Apply(&p)
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDefaultSettingsValid(t *testing.T) {
	d := DefaultSettings()
	for _, tier := range Tiers {
		s, ok := d[tier]
		if !ok {
			t.Fatalf("no settings for tier %s", tier)
		}
		if err := s.Validate(); err != nil {
			t.Errorf("tier %s: %v", tier, err)
		}
		if _, ok := PriceFor(s.Model); !ok {
			t.Errorf("tier %s: no price for %s", tier, s.Model)
		}
	}
}

func TestApplyDefaults(t *testing.T) {
	d := DefaultSettings()

	cheap := requestJSON(t, d[TierCheap], true)
	if cheap["model"] != ModelHaiku45 || cheap["max_tokens"] != float64(4096) {
		t.Errorf("cheap: model %v max_tokens %v", cheap["model"], cheap["max_tokens"])
	}
	if th := cheap["thinking"].(map[string]any); th["type"] != "disabled" {
		t.Errorf("cheap thinking = %v", th)
	}
	if _, ok := cheap["output_config"]; ok {
		t.Errorf("cheap should not send output_config: %v", cheap["output_config"])
	}

	for _, tier := range []Tier{TierMid, TierTop} {
		got := requestJSON(t, d[tier], true)
		if th := got["thinking"].(map[string]any); th["type"] != "adaptive" {
			t.Errorf("%s thinking = %v", tier, th)
		}
		oc, _ := got["output_config"].(map[string]any)
		if oc["effort"] != "medium" {
			t.Errorf("%s output_config = %v", tier, got["output_config"])
		}
	}

	for _, tier := range Tiers {
		got := requestJSON(t, d[tier], true)
		tc, _ := got["tool_choice"].(map[string]any)
		if tc["type"] != "auto" {
			t.Errorf("%s tool_choice = %v", tier, got["tool_choice"])
		}
		if _, ok := requestJSON(t, d[tier], false)["tool_choice"]; ok {
			t.Errorf("%s sent tool_choice without tools", tier)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	bad := []Settings{
		{Model: ModelOpus55, MaxTokens: 1000, Thinking: ThinkingOff},
		{Model: ModelOpus55, MaxTokens: 4000, Thinking: ThinkingBudget, ThinkingBudget: 2000},
		{Model: ModelHaiku45, MaxTokens: 4096, Thinking: ThinkingAdaptive},
		{Model: ModelHaiku45, MaxTokens: 4096, Thinking: ThinkingOff, Effort: anthropic.OutputConfigEffortLow},
		{Model: ModelHaiku45, MaxTokens: 4096, Thinking: ThinkingBudget, ThinkingBudget: 4096},
		{Model: ModelSonnet5, MaxTokens: 0, Thinking: ThinkingAdaptive},
	}
	for i, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("case %d: expected error for %+v", i, s)
		}
	}
	ok := Settings{Model: ModelHaiku45, MaxTokens: 8192, Thinking: ThinkingBudget, ThinkingBudget: 2048}
	if err := ok.Validate(); err != nil {
		t.Errorf("haiku budget thinking: %v", err)
	}
}

func TestParseTier(t *testing.T) {
	for _, tier := range Tiers {
		got, err := ParseTier(string(tier))
		if err != nil || got != tier {
			t.Errorf("ParseTier(%q) = %q, %v", tier, got, err)
		}
	}
	if _, err := ParseTier("huge"); err == nil {
		t.Error("expected error for unknown tier")
	}
}
