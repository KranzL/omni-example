package main

import (
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/bench"
)

func TestParseJevCompareArgs(t *testing.T) {
	a, err := parseJevCompareArgs([]string{"--set", "seeds", "--difficulty", "easy, hard", "--concurrency=2"})
	if err != nil {
		t.Fatal(err)
	}
	if a.set != jevSetSeeds || len(a.difficulty) != 2 || a.difficulty[1] != "hard" || a.concurrency != 2 {
		t.Errorf("args %+v", a)
	}
	for _, bad := range [][]string{{}, {"--set", "all"}, {"--set", "bench", "--concurrency", "0"}, {"--set"}, {"bench"}} {
		if _, err := parseJevCompareArgs(bad); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestParseBenchRunDifficultyAndShadow(t *testing.T) {
	a, err := parseBenchRunArgs([]string{"--config", "jev-classifier", "--difficulty", "easy,moderate,hard", "--shadow", "0.25"})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.difficulty) != 3 || a.shadow != 0.25 {
		t.Errorf("args %+v", a)
	}
	if _, err := parseBenchRunArgs([]string{"--config", "cascade-verify-jev", "--shadow", "2"}); err == nil {
		t.Error("shadow above 1: want error")
	}
	qs := []bench.Question{{ID: "e01", Difficulty: "easy"}, {ID: "x01", Difficulty: "expert"}}
	if got := filterDifficulty(qs, a.difficulty); len(got) != 1 || got[0].ID != "e01" {
		t.Errorf("filter %+v", got)
	}
	if got := filterDifficulty(qs, nil); len(got) != 2 {
		t.Errorf("empty filter must keep all, got %d", len(got))
	}
}

func TestCompareReport(t *testing.T) {
	lines := []CompareLine{
		{ID: "a", Difficulty: "easy", JevLabel: "easy", JevScore: 0.95, LLMLabel: "easy", JevLatencyMS: 300, LLMLatencyMS: 1500},
		{ID: "b", Difficulty: "moderate", JevLabel: "moderate", JevScore: 0.4, LLMLabel: "easy", JevLatencyMS: 400, LLMLatencyMS: 1400},
		{ID: "c", Difficulty: "hard", JevLabel: "hard", JevScore: 0.99, LLMLabel: "hard", JevLatencyMS: 200, LLMLatencyMS: 1600},
		{ID: "d", Difficulty: "hard", JevError: "boom"},
	}
	r := CompareReport(lines)
	for _, want := range []string{"comparable=3 of 4", "agree=2/3", "jev_vs_hand=3/3 llm_vs_hand=2/3", "jev mean=300 p50=300 p95=400"} {
		if !strings.Contains(r, want) {
			t.Errorf("report missing %q:\n%s", want, r)
		}
	}
	if !strings.Contains(r, "0.50      2/3      0/2") {
		t.Errorf("sweep row at 0.50 wrong:\n%s", r)
	}
}

func TestCompareReportExpertCountsAsHard(t *testing.T) {
	lines := []CompareLine{
		{ID: "x01", Difficulty: "expert", JevLabel: "hard", JevScore: 0.9, LLMLabel: "hard"},
		{ID: "e01", Difficulty: "easy", JevLabel: "easy", JevScore: 0.9, LLMLabel: "moderate"},
		{ID: "x02", Difficulty: "expert", JevLabel: "hard", LLMLabel: "hard", LLMError: "boom"},
	}
	r := CompareReport(lines)
	if !strings.Contains(r, "jev_vs_hand=2/2 llm_vs_hand=1/2") {
		t.Errorf("expert should match hard:\n%s", r)
	}
	jevTable := labelConfusion(lines, func(l CompareLine) string { return l.JevLabel })
	if !strings.Contains(jevTable, "  hard      0         0         1        ") {
		t.Errorf("confusion must count the expert line as hard once and skip the errored line:\n%s", jevTable)
	}
}

func TestJevCompareHealth(t *testing.T) {
	if err := jevCompareHealth([]CompareLine{{JevError: "x"}, {LLMError: "y"}}); err == nil {
		t.Fatal("no comparable lines: want error")
	}
	if err := jevCompareHealth(nil); err == nil {
		t.Fatal("no lines: want error")
	}
	if err := jevCompareHealth([]CompareLine{{JevError: "x"}, {}}); err != nil {
		t.Fatalf("one comparable line: %v", err)
	}
}
