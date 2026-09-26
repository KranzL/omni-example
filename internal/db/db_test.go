package db

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateAcceptsSelect(t *testing.T) {
	stmts := []string{
		"SELECT 1",
		"select id, email from users",
		"WITH recent AS (SELECT 1) SELECT * FROM recent",
		"  SELECT COUNT(*) FROM events  ",
	}
	for _, s := range stmts {
		if err := Validate(s); err != nil {
			t.Fatalf("Validate(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateRejectsWrites(t *testing.T) {
	stmts := []string{
		"UPDATE users SET email = 'x' WHERE id = 1",
		"INSERT INTO users (email) VALUES ('x')",
		"DELETE FROM users WHERE id = 1",
		"DROP TABLE users",
		"ALTER TABLE users ADD COLUMN x text",
		"CREATE TABLE x (id int)",
		"TRUNCATE users",
		"GRANT SELECT ON users TO omni_demo",
		"COPY users TO '/tmp/x.csv'",
		"SELECT * FROM users; DROP TABLE users",
		"SELECT * FROM users;",
		"EXPLAIN SELECT * FROM users",
		"",
	}
	for _, s := range stmts {
		if err := Validate(s); err == nil {
			t.Fatalf("Validate(%q) = nil, want error", s)
		}
	}
}

func TestQueryRejectsUpdateWithoutDB(t *testing.T) {
	_, err := Query(context.Background(), nil, "UPDATE users SET email = 'x' WHERE id = 1", 10)
	if err == nil {
		t.Fatal("Query(UPDATE) = nil, want error")
	}
}

func TestCSVRendering(t *testing.T) {
	r := Result{
		Columns: []string{"id", "name"},
		Rows:    [][]string{{"1", "a,b"}, {"2", "c"}},
	}
	got := r.CSV()
	want := "id,name\n1,\"a,b\"\n2,c\n"
	if got != want {
		t.Fatalf("CSV() = %q, want %q", got, want)
	}
}

func TestQueryIntegration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is absent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := Query(ctx, pool, "UPDATE users SET email = 'x' WHERE id = 1", 10); err == nil {
		t.Fatal("Query(UPDATE) = nil, want error")
	}
	capped, err := Query(ctx, pool, "SELECT id FROM users ORDER BY id", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !capped.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if len(capped.Rows) != 3 {
		t.Fatalf("len(Rows) = %d, want 3", len(capped.Rows))
	}
	small, err := Query(ctx, pool, "SELECT id FROM distribution_centers ORDER BY id", 100)
	if err != nil {
		t.Fatal(err)
	}
	if small.Truncated {
		t.Fatal("Truncated = true, want false")
	}
	if len(small.Rows) == 0 || len(small.Rows) > 100 {
		t.Fatalf("len(Rows) = %d, want between 1 and 100", len(small.Rows))
	}
	if !strings.Contains(small.CSV(), "id") {
		t.Fatalf("CSV() = %q, want header with id", small.CSV())
	}
}

func TestValidateIgnoresLiteralsAndComments(t *testing.T) {
	stmts := []string{
		"SELECT name FROM products WHERE name ILIKE '%drop%'",
		"SELECT COUNT(*) FROM products WHERE brand = 'Update Apparel'",
		"SELECT string_agg(name, ';') FROM products",
		"-- revenue by month\nSELECT 1",
		"/* delete me */ SELECT 1",
		"(SELECT 1) UNION (SELECT 2)",
		`SELECT "into" FROM (SELECT 1 AS "into") t`,
		"SELECT $$it's; drop$$",
		"SELECT 'it''s; insert'",
	}
	for _, s := range stmts {
		if err := Validate(s); err != nil {
			t.Fatalf("Validate(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateRejectsSideEffectFunctions(t *testing.T) {
	stmts := []string{
		"SELECT set_config('search_path', 'pg_catalog', false)",
		"SELECT pg_advisory_lock(1)",
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity",
		"SELECT pg_sleep(29)",
		"SELECT * INTO new_users FROM users",
		"SELECT 1 /* ; */ ; DROP TABLE users",
		"SELECT 'x'; DELETE FROM users",
	}
	for _, s := range stmts {
		if err := Validate(s); err == nil {
			t.Fatalf("Validate(%q) = nil, want error", s)
		}
	}
}
