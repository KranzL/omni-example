package db

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"
)

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.sqlite")
	setup, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer setup.Close()
	for _, stmt := range []string{
		"CREATE TABLE heroes (id INTEGER PRIMARY KEY, name TEXT, weight REAL)",
		"INSERT INTO heroes (id, name, weight) VALUES (1, 'Amazo', 80.5), (2, 'Bolt', NULL), (3, 'Cipher', 72.0)",
	} {
		if _, err := setup.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	sqldb, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	return sqldb
}

func TestSQLiteBackendQuery(t *testing.T) {
	b := SQLiteBackend{DB: openTestSQLite(t)}
	res, err := b.Query(context.Background(), "SELECT id, name, weight FROM heroes ORDER BY id", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Columns) != 3 || res.Columns[0] != "id" {
		t.Fatalf("columns = %v", res.Columns)
	}
	if len(res.Rows) != 3 || res.Rows[0][1] != "Amazo" || res.Rows[1][2] != "" {
		t.Fatalf("rows = %v", res.Rows)
	}
	if res.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestSQLiteBackendRowCap(t *testing.T) {
	b := SQLiteBackend{DB: openTestSQLite(t)}
	res, err := b.Query(context.Background(), "SELECT id FROM heroes", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 2 || !res.Truncated {
		t.Fatalf("rows = %d truncated = %v, want 2 rows truncated", len(res.Rows), res.Truncated)
	}
	if _, err := b.Query(context.Background(), "SELECT 1", 0); err == nil {
		t.Fatal("maxRows 0: want error")
	}
}

func TestSQLiteBackendRejectsWrites(t *testing.T) {
	b := SQLiteBackend{DB: openTestSQLite(t)}
	for _, stmt := range []string{
		"UPDATE heroes SET name = 'x' WHERE id = 1",
		"INSERT INTO heroes (id) VALUES (9)",
		"DELETE FROM heroes",
		"DROP TABLE heroes",
		"SELECT * FROM heroes;",
	} {
		if _, err := b.Query(context.Background(), stmt, 10); err == nil {
			t.Fatalf("Query(%q) = nil, want error", stmt)
		}
	}
}

func TestSQLiteBackendIsReadOnly(t *testing.T) {
	sqldb := openTestSQLite(t)
	if _, err := sqldb.Exec("INSERT INTO heroes (id) VALUES (9)"); err == nil {
		t.Fatal("raw INSERT through read-only handle: want error")
	}
}

func TestSQLiteBackendSQLError(t *testing.T) {
	b := SQLiteBackend{DB: openTestSQLite(t)}
	if _, err := b.Query(context.Background(), "SELECT nope FROM heroes", 10); err == nil {
		t.Fatal("bad column: want error")
	}
}

func TestOpenSQLiteMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	if _, err := OpenSQLite(path); err == nil {
		t.Fatal("missing file: want error")
	}
}

func TestBackendsSatisfyInterface(t *testing.T) {
	var _ Backend = PGBackend{}
	var _ Backend = SQLiteBackend{DB: openTestSQLite(t)}
}

func TestOpenSQLitePathWithQueryCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "odd?name#1.sqlite")
	setup, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: path}).String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	setup.Close()
	sqldb, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	var n int
	if err := sqldb.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if _, err := sqldb.Exec("INSERT INTO t VALUES (1)"); err == nil {
		t.Fatal("write through read-only handle: want error")
	}
}
