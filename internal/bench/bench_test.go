package bench

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/db"
)

const questionsPath = "../../bench/questions.yaml"

func loadBenchmark(t *testing.T) []Question {
	t.Helper()
	qs, err := Load(questionsPath)
	if err != nil {
		t.Fatalf("Load(%s) = %v", questionsPath, err)
	}
	return qs
}

func TestBenchmarkCounts(t *testing.T) {
	qs := loadBenchmark(t)
	if len(qs) != 32 {
		t.Fatalf("len(questions) = %d, want 32", len(qs))
	}
	wantIDs := map[string]bool{}
	for i := 1; i <= 8; i++ {
		wantIDs["e"+two(i)] = true
		wantIDs["m"+two(i)] = true
		wantIDs["h"+two(i)] = true
		wantIDs["x"+two(i)] = true
	}
	counts := map[string]int{}
	for _, q := range qs {
		if !wantIDs[q.ID] {
			t.Errorf("unexpected id %q", q.ID)
		}
		delete(wantIDs, q.ID)
		counts[q.Difficulty]++
	}
	for id := range wantIDs {
		t.Errorf("missing id %q", id)
	}
	for _, d := range []string{DifficultyEasy, DifficultyModerate, DifficultyHard, DifficultyExpert} {
		if counts[d] != 8 {
			t.Errorf("difficulty %q count = %d, want 8", d, counts[d])
		}
	}
}

func two(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestBenchmarkWindowsPinned(t *testing.T) {
	qs := loadBenchmark(t)
	unpinned := map[string]bool{"e01": true, "e02": true, "e03": true, "e06": true}
	for _, q := range qs {
		pinned := strings.Contains(q.SQL, "2023") || strings.Contains(q.SQL, "2024")
		if pinned && unpinned[q.ID] {
			t.Errorf("%s is allowlisted as time-invariant but pins a year", q.ID)
		}
		if !pinned && !unpinned[q.ID] {
			t.Errorf("%s aggregates without a closed 2023 or 2024 window", q.ID)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	tol := 0.01
	base := Question{
		ID:         "e01",
		Difficulty: DifficultyEasy,
		Text:       "q",
		SQL:        "SELECT 1",
		AnswerType: TypeNumber,
		Tolerance:  &tol,
		Note:       "n",
	}
	cases := []struct {
		name   string
		mutate func(*Question)
	}{
		{"bad id", func(q *Question) { q.ID = "x1" }},
		{"difficulty mismatch", func(q *Question) { q.Difficulty = DifficultyHard }},
		{"empty text", func(q *Question) { q.Text = " " }},
		{"empty sql", func(q *Question) { q.SQL = " " }},
		{"bad answer type", func(q *Question) { q.AnswerType = "paragraph" }},
		{"empty note", func(q *Question) { q.Note = "" }},
		{"write sql", func(q *Question) { q.SQL = "DELETE FROM users" }},
		{"semicolon sql", func(q *Question) { q.SQL = "SELECT 1;" }},
	}
	for _, c := range cases {
		q := base
		c.mutate(&q)
		if err := Validate([]Question{q}); err == nil {
			t.Errorf("%s: Validate = nil, want error", c.name)
		}
	}
	badTol := 1.5
	q := base
	q.Tolerance = &badTol
	if err := Validate([]Question{q}); err == nil {
		t.Error("tolerance 1.5: Validate = nil, want error")
	}
	if err := Validate(nil); err == nil {
		t.Error("empty list: Validate = nil, want error")
	}
	other := base
	other.ID = "e02"
	if err := Validate([]Question{base, base}); err == nil {
		t.Error("duplicate ids: Validate = nil, want error")
	}
	if err := Validate([]Question{base, other}); err != nil {
		t.Errorf("valid pair: Validate = %v, want nil", err)
	}
}

func TestEffectiveToleranceDefaults(t *testing.T) {
	n := Question{AnswerType: TypeNumber}
	if got := n.EffectiveTolerance(); got != DefaultNumberTolerance {
		t.Errorf("number default = %v, want %v", got, DefaultNumberTolerance)
	}
	s := Question{AnswerType: TypeString}
	if got := s.EffectiveTolerance(); got != 0 {
		t.Errorf("string default = %v, want 0", got)
	}
	explicit := 0.05
	e := Question{AnswerType: TypeNumber, Tolerance: &explicit}
	if got := e.EffectiveTolerance(); got != explicit {
		t.Errorf("explicit tolerance = %v, want %v", got, explicit)
	}
}

func TestBuildAnswer(t *testing.T) {
	num := Question{AnswerType: TypeNumber}
	v, err := BuildAnswer(num, db.Result{Columns: []string{"c"}, Rows: [][]string{{"42.5"}}})
	if err != nil {
		t.Fatal(err)
	}
	if v.(float64) != 42.5 {
		t.Errorf("number = %v, want 42.5", v)
	}
	if _, err := BuildAnswer(num, db.Result{Columns: []string{"c"}, Rows: [][]string{{"abc"}}}); err == nil {
		t.Error("non-numeric number answer: want error")
	}
	if _, err := BuildAnswer(num, db.Result{Columns: []string{"c"}, Rows: [][]string{{"1"}, {"2"}}}); err == nil {
		t.Error("two-row number answer: want error")
	}
	str := Question{AnswerType: TypeString}
	v, err = BuildAnswer(str, db.Result{Columns: []string{"c"}, Rows: [][]string{{"hi"}}})
	if err != nil || v.(string) != "hi" {
		t.Errorf("string = %v, %v; want hi, nil", v, err)
	}
	lst := Question{AnswerType: TypeRankedList}
	v, err = BuildAnswer(lst, db.Result{Columns: []string{"c"}, Rows: [][]string{{"a"}, {"b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := v.([]string); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("ranked_list = %v, want [a b]", got)
	}
	if _, err := BuildAnswer(lst, db.Result{Columns: []string{"c"}, Rows: nil}); err == nil {
		t.Error("empty list answer: want error")
	}
	tab := Question{AnswerType: TypeTable}
	v, err = BuildAnswer(tab, db.Result{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	ta := v.(TableAnswer)
	if len(ta.Columns) != 2 || len(ta.Rows) != 1 {
		t.Errorf("table = %+v, want 2 columns and 1 row", ta)
	}
	if _, err := BuildAnswer(tab, db.Result{Columns: []string{"a"}}); err == nil {
		t.Error("rowless table answer: want error")
	}
}

func TestFindBenchDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := FindBenchDir(cwd)
	if dir == "" {
		t.Fatal("FindBenchDir = empty, want bench dir")
	}
	if _, err := os.Stat(dir + "/" + QuestionsFile); err != nil {
		t.Fatalf("questions file missing in %s: %v", dir, err)
	}
}

func TestGroundTruthExecutes(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is absent")
	}
	qs := loadBenchmark(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, q := range qs {
		start := time.Now()
		res, err := db.Query(ctx, pool, q.SQL, MaxAnswerRows)
		elapsed := time.Since(start)
		if err != nil {
			t.Errorf("%s: Query = %v", q.ID, err)
			continue
		}
		if res.Truncated {
			t.Errorf("%s: answer exceeds %d rows", q.ID, MaxAnswerRows)
			continue
		}
		if elapsed >= 30*time.Second {
			t.Errorf("%s: took %.2fs, want under 30s", q.ID, elapsed.Seconds())
		}
		if _, err := BuildAnswer(q, res); err != nil {
			t.Errorf("%s: BuildAnswer = %v", q.ID, err)
		}
	}
}
