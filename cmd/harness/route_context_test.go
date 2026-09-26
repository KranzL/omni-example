package main

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/llm"
)

func TestRouteContextFlagsOnlyPerUserSequencing(t *testing.T) {
	qs, err := bench.Load(filepath.Join("..", "..", "bench", bench.QuestionsFile))
	if err != nil {
		t.Fatal(err)
	}
	var flagged []string
	for _, q := range qs {
		if len(routeContextHints(q.Text)) > 0 {
			flagged = append(flagged, q.ID)
		}
		if withRouteContext(q.Text) == q.Text && len(routeContextHints(q.Text)) > 0 {
			t.Fatalf("%s flagged but question unchanged", q.ID)
		}
	}
	sort.Strings(flagged)
	want := []string{"x01", "x03", "x04", "x05", "x06", "x07", "x08"}
	if strings.Join(flagged, ",") != strings.Join(want, ",") {
		t.Fatalf("flagged %v, want %v", flagged, want)
	}
}

func TestRouteContextAppendsAfterQuestion(t *testing.T) {
	q := "What was the median days from first to second non-cancelled order for users acquired through Facebook in 2024?"
	got := withRouteContext(q)
	if !strings.HasPrefix(got, q+"\n\n") || !strings.Contains(got, "Rank and difference orders, never raw items.") {
		t.Fatalf("hint not appended after the question: %q", got)
	}
	plain := "How many users signed up in 2024?"
	if withRouteContext(plain) != plain {
		t.Fatalf("unflagged question changed")
	}
}

func TestParseRouteContextAndCheapThinking(t *testing.T) {
	opts, err := parseBenchRunArgs([]string{"--config", "always-cheap", "--route-context", "--cheap-thinking", "4096", "--tag", "exp"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.routeCtx || opts.cheapThink != 4096 {
		t.Fatalf("flags not parsed: %+v", opts)
	}
	if _, err := parseBenchRunArgs([]string{"--config", "always-cheap", "--route-context"}); err == nil {
		t.Fatalf("--route-context without --tag must fail")
	}
	if _, err := parseBenchRunArgs([]string{"--config", "always-cheap", "--cheap-thinking", "512", "--tag", "x"}); err == nil {
		t.Fatalf("budget under 1024 must fail")
	}
}

func TestEnableCheapThinkingKeepsOtherTiers(t *testing.T) {
	c := llm.NewClient("test-key", "")
	mid := c.Settings(llm.TierMid)
	if err := enableCheapThinking(c, 4096); err != nil {
		t.Fatal(err)
	}
	s := c.Settings(llm.TierCheap)
	if s.Thinking != llm.ThinkingBudget || s.ThinkingBudget != 4096 || s.MaxTokens != 8192 {
		t.Fatalf("cheap settings %+v", s)
	}
	if c.Settings(llm.TierMid) != mid {
		t.Fatalf("mid tier changed")
	}
	if llm.DefaultSettings()[llm.TierCheap].Thinking != llm.ThinkingOff {
		t.Fatalf("default cheap tier changed")
	}
}
