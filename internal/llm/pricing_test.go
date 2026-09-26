package llm

import (
	"math"
	"testing"
)

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("cost = %.10f, want %.10f", got, want)
	}
}

func TestCostPerModel(t *testing.T) {
	u := Usage{InputTokens: 1000, OutputTokens: 500, CacheCreationInputTokens: 2000, CacheReadInputTokens: 10000}
	cases := []struct {
		model string
		want  float64
	}{
		{ModelHaiku45, 0.007},
		{"claude-haiku-4-5-20251001", 0.007},
		{ModelSonnet5, 0.014},
		{ModelOpus55, 0.026},
	}
	for _, c := range cases {
		t.Run(c.model, func(t *testing.T) {
			got, err := Cost(c.model, u)
			if err != nil {
				t.Fatal(err)
			}
			approx(t, got, c.want)
		})
	}
}

func TestCostSingleCategory(t *testing.T) {
	million := int64(1_000_000)
	for model, p := range Prices {
		approx(t, p.Cost(Usage{InputTokens: million}), p.Input)
		approx(t, p.Cost(Usage{OutputTokens: million}), p.Output)
		approx(t, p.Cost(Usage{CacheCreationInputTokens: million}), p.CacheWrite5m)
		approx(t, p.Cost(Usage{CacheCreationInputTokens: million, CacheCreation1hTokens: million}), p.CacheWrite1h)
		approx(t, p.Cost(Usage{CacheReadInputTokens: million}), p.CacheRead)
		if p.CacheWrite5m != p.Input*1.25 {
			t.Errorf("%s: 5m cache write %.4f is not 1.25x input", model, p.CacheWrite5m)
		}
	}
}

func TestCostMixedUsage(t *testing.T) {
	u := Usage{
		InputTokens:              300,
		OutputTokens:             1200,
		CacheCreationInputTokens: 5000,
		CacheCreation1hTokens:    2000,
		CacheReadInputTokens:     40000,
	}
	got, err := Cost(ModelOpus55, u)
	if err != nil {
		t.Fatal(err)
	}
	approx(t, got, 0.0642)
	if u.PromptTokens() != 45300 {
		t.Fatalf("prompt tokens = %d, want 45300", u.PromptTokens())
	}
}

func TestCostUnknownModel(t *testing.T) {
	if _, err := Cost("claude-unknown", Usage{InputTokens: 1}); err == nil {
		t.Fatal("expected error for unknown model")
	}
}

func TestNormalizeModel(t *testing.T) {
	cases := map[string]string{
		"claude-haiku-4-5-20251001": "claude-haiku-4-5",
		"claude-haiku-4-5":          "claude-haiku-4-5",
		"claude-opus-5-5":           "claude-opus-5-5",
		"claude-sonnet-5":           "claude-sonnet-5",
	}
	for in, want := range cases {
		if got := NormalizeModel(in); got != want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}
