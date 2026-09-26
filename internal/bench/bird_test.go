package bench

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/KranzL/omni-example/internal/llm"
	_ "modernc.org/sqlite"
)

func birdTestQuestion() Question {
	tol := 0.01
	return Question{
		ID:         "b01",
		Difficulty: DifficultySimple,
		Text:       "How many heroes are there?",
		SQL:        "SELECT COUNT(*) FROM hero",
		AnswerType: TypeNumber,
		Tolerance:  &tol,
		Note:       "count",
		DbID:       "tiny",
		Evidence:   "count means COUNT(*)",
		BirdID:     42,
	}
}

func TestValidateBirdQuestion(t *testing.T) {
	if err := ValidateBirdQuestion(birdTestQuestion()); err != nil {
		t.Fatalf("valid question: %s", err)
	}
	cases := []struct {
		name   string
		change func(*Question)
	}{
		{"id", func(q *Question) { q.ID = "e01" }},
		{"difficulty", func(q *Question) { q.Difficulty = "easy" }},
		{"text", func(q *Question) { q.Text = "" }},
		{"sql", func(q *Question) { q.SQL = "DELETE FROM hero" }},
		{"answer_type", func(q *Question) { q.AnswerType = "mystery" }},
		{"note", func(q *Question) { q.Note = "" }},
		{"db_id", func(q *Question) { q.DbID = "" }},
		{"evidence", func(q *Question) { q.Evidence = "" }},
		{"bird_id", func(q *Question) { q.BirdID = 0 }},
	}
	for _, c := range cases {
		q := birdTestQuestion()
		c.change(&q)
		if err := ValidateBirdQuestion(q); err == nil {
			t.Fatalf("%s: want error", c.name)
		}
	}
}

func TestValidateBirdDuplicates(t *testing.T) {
	q := birdTestQuestion()
	other := birdTestQuestion()
	other.ID = "b02"
	if err := ValidateBird([]Question{q, other}); err == nil {
		t.Fatal("duplicate bird_id: want error")
	}
	other.BirdID = 43
	if err := ValidateBird([]Question{q, other}); err != nil {
		t.Fatalf("distinct ids: %s", err)
	}
	other.ID = "b01"
	if err := ValidateBird([]Question{q, other}); err == nil {
		t.Fatal("duplicate id: want error")
	}
	if err := ValidateBird(nil); err == nil {
		t.Fatal("empty list: want error")
	}
}

func TestLoadBirdSample(t *testing.T) {
	path := filepath.Join("..", "..", "bench", "bird", "questions.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("bird sample not present")
	}
	qs, err := LoadBird(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 30 {
		t.Fatalf("sample = %d questions, want 30", len(qs))
	}
	counts := map[string]int{}
	for _, q := range qs {
		counts[q.Difficulty]++
	}
	for _, d := range []string{DifficultySimple, DifficultyModerate, DifficultyChallenging} {
		if counts[d] != 10 {
			t.Fatalf("%s = %d, want 10", d, counts[d])
		}
	}
}

func openBirdEXTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tiny.sqlite")
	setup, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer setup.Close()
	for _, stmt := range []string{
		"CREATE TABLE hero (id INTEGER PRIMARY KEY, hero_name TEXT)",
		"INSERT INTO hero (id, hero_name) VALUES (1, 'Amazo'), (2, 'Bolt')",
	} {
		if _, err := setup.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	sqldb, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	return sqldb
}

func TestBirdEX(t *testing.T) {
	g := BirdEX{DBs: map[string]*sql.DB{"tiny": openBirdEXTestDB(t)}}
	ctx := context.Background()
	q := birdTestQuestion()
	q.DbID = "tiny"
	q.SQL = "SELECT hero_name FROM hero"
	ok, err := g.ExecCorrect(ctx, q, "SELECT hero_name FROM hero ORDER BY id DESC")
	if err != nil || !ok {
		t.Fatalf("reordered without ORDER BY = %v, %v; want true", ok, err)
	}
	ok, err = g.ExecCorrect(ctx, q, "SELECT hero_name FROM hero WHERE id = 1")
	if err != nil || ok {
		t.Fatalf("subset = %v, %v; want false", ok, err)
	}
	ok, err = g.ExecCorrect(ctx, q, "SELECT nope FROM hero")
	if err != nil || ok {
		t.Fatalf("broken SQL = %v, %v; want false without error", ok, err)
	}
	ok, err = g.ExecCorrect(ctx, q, "")
	if err != nil || ok {
		t.Fatalf("empty SQL = %v, %v; want false", ok, err)
	}
	ok, err = g.ExecCorrect(ctx, q, "SELECT hero_name FROM hero;")
	if err != nil || !ok {
		t.Fatalf("trailing semicolon = %v, %v; want true", ok, err)
	}
	ordered := q
	ordered.SQL = "SELECT hero_name FROM hero ORDER BY id"
	ok, err = g.ExecCorrect(ctx, ordered, "SELECT hero_name FROM hero ORDER BY id DESC")
	if err != nil || ok {
		t.Fatalf("flipped order with ORDER BY = %v, %v; want false", ok, err)
	}
	unknown := q
	unknown.DbID = "missing"
	if _, err := g.ExecCorrect(ctx, unknown, "SELECT 1"); err == nil {
		t.Fatal("unknown db: want error")
	}
	broken := q
	broken.SQL = "SELECT nope FROM hero"
	if _, err := g.ExecCorrect(ctx, broken, "SELECT hero_name FROM hero"); err == nil {
		t.Fatal("broken gold: want error")
	}
}

func TestExpectedTierBird(t *testing.T) {
	if got := ExpectedTier(DifficultySimple); got != llm.TierCheap {
		t.Fatalf("simple = %q", got)
	}
	if got := ExpectedTier(DifficultyChallenging); got != llm.TierTop {
		t.Fatalf("challenging = %q", got)
	}
}
