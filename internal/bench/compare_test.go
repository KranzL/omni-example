package bench

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/KranzL/omni-example/internal/llm"
)

func loadSide(t *testing.T, label, name string) Side {
	t.Helper()
	path := filepath.Join("testdata", name)
	lines, err := ReadLines(path)
	if err != nil {
		t.Fatal(err)
	}
	return NewSide(label, path, lines)
}

func TestComputeOmniSingleRun(t *testing.T) {
	s := loadSide(t, "a", "compare_a.jsonl")
	m := s.Summary.Omni
	if m == nil {
		t.Fatal("no omni metrics")
	}
	if m.Questions != 3 || m.Repeats != 1 {
		t.Errorf("questions %d repeats %d", m.Questions, m.Repeats)
	}
	if m.TotalTokens != 6000 || m.TokensPerCorrect != 3000 {
		t.Errorf("tokens %d per correct %v, want 6000 and 3000", m.TotalTokens, m.TokensPerCorrect)
	}
	if m.MedianLatencyMS != 5000 {
		t.Errorf("median %d, want 5000", m.MedianLatencyMS)
	}
	if m.ErrorFree != 2 {
		t.Errorf("error free %d, want 2", m.ErrorFree)
	}
	if m.Consistent != nil || m.AnswersChanged != nil || m.ConsistencyRate != nil {
		t.Errorf("consistency set on a single run: %+v", m)
	}
}

func TestComputeOmniRepeats(t *testing.T) {
	route := llm.Usage{InputTokens: 10, OutputTokens: 5}
	lines := []Line{
		{ID: "e01", Repeat: 1, Correct: true, Submitted: true, Answer: "10", LatencyMS: 100, Tokens: llm.Usage{InputTokens: 100}, RouteTokens: route},
		{ID: "e02", Repeat: 1, Correct: true, Submitted: true, Answer: "a", LatencyMS: 400},
		{ID: "e03", Repeat: 1, Correct: false, Submitted: false, Answer: "x", LatencyMS: 300},
		{ID: "e01", Repeat: 2, Correct: true, Submitted: true, Answer: "10", LatencyMS: 200, Tokens: llm.Usage{InputTokens: 100}, RouteTokens: route},
		{ID: "e02", Repeat: 2, Correct: false, Submitted: true, Error: "boom", Answer: "b", LatencyMS: 500},
		{ID: "e03", Repeat: 2, Correct: false, Submitted: true, Answer: "Y", LatencyMS: 600},
	}
	m := ComputeOmni(lines)
	if m.Repeats != 2 || m.Questions != 3 {
		t.Fatalf("repeats %d questions %d", m.Repeats, m.Questions)
	}
	if m.Consistent == nil || *m.Consistent != 2 || *m.AnswersChanged != 1 || *m.AnswerTextsChanged != 2 {
		t.Errorf("consistency %+v", m)
	}
	if *m.ConsistencyRate != 2.0/3.0 {
		t.Errorf("rate %v", *m.ConsistencyRate)
	}
	if m.MedianLatencyMS != 350 {
		t.Errorf("median %d, want 350", m.MedianLatencyMS)
	}
	if m.ErrorFree != 4 || m.ErrorFreeRate != 4.0/6.0 {
		t.Errorf("error free %d rate %v", m.ErrorFree, m.ErrorFreeRate)
	}
	if m.TotalTokens != 230 || m.TokensPerCorrect != 230.0/3.0 {
		t.Errorf("tokens %d per correct %v", m.TotalTokens, m.TokensPerCorrect)
	}
}

func TestCompareFlips(t *testing.T) {
	c := Compare(loadSide(t, "before", "compare_a.jsonl"), loadSide(t, "after", "compare_b.jsonl"))
	if len(c.Flips) != 2 {
		t.Fatalf("flips %+v", c.Flips)
	}
	m01, x04 := c.Flips[0], c.Flips[1]
	if m01.ID != "m01" || m01.A.Verdict() != "pass" || m01.B.Verdict() != "fail" || m01.B.Answer != "Jeans" || m01.B.CostUSD != 0.016 {
		t.Errorf("m01 flip %+v", m01)
	}
	if x04.ID != "x04" || x04.Difficulty != DifficultyExpert || x04.A.Verdict() != "fail" || x04.B.Verdict() != "pass" || x04.A.Answer != "148.08" {
		t.Errorf("x04 flip %+v", x04)
	}
	out := c.Format()
	for _, want := range []string{
		"verdict flips: 2",
		"tokens per correct",
		"3000",
		"4000",
		"median latency",
		"5000ms",
		"8000ms",
		"error-free rate",
		"0.667 (2/3)",
		"x04  expert",
		"148.08",
		"149.95",
		"$0.013000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "consistency across runs") {
		t.Errorf("single-run comparison prints consistency\n%s", out)
	}
}

func TestCompareMissingQuestion(t *testing.T) {
	a := NewSide("a", "a.jsonl", []Line{{ID: "e01", Correct: true, Repeat: 1}})
	b := NewSide("b", "b.jsonl", []Line{{ID: "e01", Correct: true, Repeat: 1}, {ID: "e02", Correct: true, Repeat: 1}})
	c := Compare(a, b)
	if len(c.Flips) != 1 || c.Flips[0].ID != "e02" || c.Flips[0].A.Verdict() != "missing" {
		t.Errorf("flips %+v", c.Flips)
	}
}

func TestAppendOmniKeepsExistingFields(t *testing.T) {
	old := []byte("{\n  \"config\": \"always-cheap\",\n  \"p95_latency_ms\": 10\n}\n")
	m := OmniMetrics{Questions: 1, Repeats: 1, TotalTokens: 5, TokensPerCorrect: 5, MedianLatencyMS: 10, ErrorFree: 1, ErrorFreeRate: 1}
	got, err := AppendOmni(old, m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(got), "{\n  \"config\": \"always-cheap\",\n  \"p95_latency_ms\": 10,\n  \"omni\": {\n    \"questions\": 1,") {
		t.Errorf("unexpected output\n%s", got)
	}
	m.TotalTokens = 7
	again, err := AppendOmni(got, m)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(again), "\"omni\"") != 1 || !strings.Contains(string(again), "\"total_tokens\": 7") {
		t.Errorf("second append\n%s", again)
	}
}

func TestAppendOmniKeepsTrailingFields(t *testing.T) {
	old := []byte("{\n  \"config\": \"always-cheap\",\n  \"omni\": {\n    \"questions\": 1\n  },\n  \"regraded\": {\n    \"at\": \"2026-09-24T16:04:22Z\",\n    \"lines\": 24\n  }\n}\n")
	m := OmniMetrics{Questions: 1, Repeats: 1, TotalTokens: 5, TokensPerCorrect: 5, MedianLatencyMS: 10, ErrorFree: 1, ErrorFreeRate: 1}
	got, err := AppendOmni(old, m)
	if err != nil {
		t.Fatal(err)
	}
	out := string(got)
	if strings.Count(out, "\"omni\"") != 1 || !strings.Contains(out, "\"total_tokens\": 5") {
		t.Errorf("omni block not replaced\n%s", out)
	}
	if !strings.Contains(out, "\"regraded\": {\n    \"at\": \"2026-09-24T16:04:22Z\",\n    \"lines\": 24\n  }") {
		t.Errorf("regraded block not preserved\n%s", out)
	}
	if strings.Index(out, "\"omni\"") > strings.Index(out, "\"regraded\"") {
		t.Errorf("omni block moved after regraded\n%s", out)
	}
}

func TestOneLineCutsOnRuneBoundary(t *testing.T) {
	for n := 1; n <= 7; n++ {
		got := OneLine("éééé", n)
		if !utf8.ValidString(got) {
			t.Fatalf("OneLine(n=%d) = %q, not valid UTF-8", n, got)
		}
	}
	if got := OneLine("aé", 2); got != "a..." {
		t.Errorf("OneLine = %q, want a...", got)
	}
	if got := oneLine("a\nb", 10); got != "a | b" {
		t.Errorf("oneLine = %q", got)
	}
}
