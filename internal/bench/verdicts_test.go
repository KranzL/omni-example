package bench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegradeVerdictsFile(t *testing.T) {
	qs := []Question{
		{ID: "e01", Difficulty: DifficultyEasy, AnswerType: TypeNumber},
		{ID: "e02", Difficulty: DifficultyEasy, AnswerType: TypeFreeText},
	}
	body := strings.Join([]string{
		`{"id":"e01","difficulty":"easy","repeat":1,"correct":false,"correct_strict":false,"fail_reason":"stale_verdict: x","answer":"42","expected":42,"submitted":true,"attempts":[{"tier":"cheap","correct":false,"answer":"41"},{"tier":"mid","correct":false,"answer":"42"}]}`,
		`{"id":"e01","difficulty":"easy","repeat":2,"correct":true,"answer":"about 7","expected":42,"submitted":true}`,
		`{"id":"e02","difficulty":"easy","repeat":1,"correct":false,"correct_strict":false,"answer":"text","expected":"other","submitted":true,"judge_verdict":"fail"}`,
	}, "\n") + "\n"
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, changes, err := RegradeVerdictsFile(path, qs)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 4 {
		t.Fatalf("changes = %+v, want 4", changes)
	}
	if !lines[0].Correct || !lines[0].CorrectStrict || lines[0].FailReason != "" {
		t.Errorf("line 1 = %+v", lines[0])
	}
	if lines[0].Attempts[0].Correct || !lines[0].Attempts[1].Correct {
		t.Errorf("attempts = %+v", lines[0].Attempts)
	}
	if lines[1].Correct || lines[1].CorrectStrict || !strings.HasPrefix(lines[1].FailReason, ReasonWrongNumber) {
		t.Errorf("line 2 = %+v", lines[1])
	}
	if lines[2].Correct {
		t.Errorf("free text verdict changed: %+v", lines[2])
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{
		`{"id":"e01","difficulty":"easy","repeat":1,"correct":true,"correct_strict":true,"answer":"42","expected":42,"submitted":true,"attempts":[{"tier":"cheap","correct":false,"answer":"41"},{"tier":"mid","correct":true,"answer":"42"}]}`,
		`{"id":"e01","difficulty":"easy","repeat":2,"correct":false,"correct_strict":false,"fail_reason":"` + lines[1].FailReason + `","answer":"about 7","expected":42,"submitted":true}`,
		`{"id":"e02","difficulty":"easy","repeat":1,"correct":false,"correct_strict":false,"fail_reason":"judge_rejected: fail","answer":"text","expected":"other","submitted":true,"judge_verdict":"fail"}`,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d\ngot  %s\nwant %s", i+1, got[i], want[i])
		}
	}
}

func TestResummarizeVerdicts(t *testing.T) {
	old := Summary{Config: "c", Provider: "venice", Source: "s.jsonl", Total: 2, Correct: 0, TotalCostUSD: 1, ByDifficulty: map[string]DifficultySummary{}}
	lines := []Line{
		{ID: "e01", Difficulty: DifficultyEasy, Correct: true, CorrectStrict: true, CostUSD: 0.5},
		{ID: "e02", Difficulty: DifficultyEasy, CostUSD: 0.5, FailReason: "wrong_number: x"},
	}
	s := ResummarizeVerdicts(old, lines, RegradeInfo{At: "2026-01-01T00:00:00Z", VerdictsChanged: 1})
	if s.Correct != 1 || s.Accuracy != 0.5 || s.CostPerCorrectUSD != 1 || s.Provider != "venice" || s.ByDifficulty[DifficultyEasy].Correct != 1 {
		t.Errorf("summary = %+v", s)
	}
	if s.Regraded == nil || s.Regraded.VerdictsChanged != 1 || s.FailReasons["wrong_number"] != 1 {
		t.Errorf("regraded = %+v, fail = %v", s.Regraded, s.FailReasons)
	}
}

func TestRegradeVerdictsFileReplacesReasonAfterEX(t *testing.T) {
	qs := []Question{{ID: "e01", Difficulty: DifficultyEasy, AnswerType: TypeNumber}}
	body := `{"id":"e01","difficulty":"easy","repeat":1,"correct":false,"correct_strict":false,"ex_correct":true,"fail_reason":"stale_verdict: x","answer":"42","expected":42,"submitted":true}` + "\n"
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RegradeVerdictsFile(path, qs); err != nil {
		t.Fatal(err)
	}
	back, err := ReadLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 || !back[0].Correct || back[0].FailReason != "" || back[0].EXCorrect == nil || !*back[0].EXCorrect {
		t.Fatalf("reread = %+v", back)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "stale_verdict") {
		t.Fatalf("stale reason kept: %s", data)
	}
}
