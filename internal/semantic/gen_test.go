package semantic

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openGenTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gen.sqlite")
	sqldb, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	for _, stmt := range []string{
		"CREATE TABLE publisher (id INTEGER PRIMARY KEY, publisher_name TEXT)",
		"INSERT INTO publisher (id, publisher_name) VALUES (1, 'Marvel'), (2, 'DC')",
		"CREATE TABLE hero (id INTEGER PRIMARY KEY, hero_name TEXT NOT NULL, publisher_id INTEGER REFERENCES publisher(id), weight_kg REAL)",
		"INSERT INTO hero (id, hero_name, publisher_id, weight_kg) VALUES (1, 'Amazo', 1, 80.5), (2, 'Bolt', 2, 72.0)",
	} {
		if _, err := sqldb.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return sqldb
}

func TestGenerateSQLite(t *testing.T) {
	s, err := GenerateSQLite(openGenTestDB(t), GenOptions{Dataset: "test db", Title: "the test database"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Schema != "main" {
		t.Fatalf("schema = %q, want main", s.Schema)
	}
	if len(s.Tables) != 2 {
		t.Fatalf("tables = %d, want 2", len(s.Tables))
	}
	byName := map[string]Table{}
	for _, tbl := range s.Tables {
		byName[tbl.Name] = tbl
	}
	hero := byName["hero"]
	if hero.PrimaryKey != "id" {
		t.Fatalf("hero primary key = %q, want id", hero.PrimaryKey)
	}
	if hero.ApproxRows != 2 {
		t.Fatalf("hero rows = %d, want 2", hero.ApproxRows)
	}
	if len(hero.ForeignKeys) != 1 || hero.ForeignKeys[0].References != "publisher.id" {
		t.Fatalf("hero fks = %+v", hero.ForeignKeys)
	}
	if len(s.Enums) == 0 {
		t.Fatal("no enums generated")
	}
	enumCols := map[string]bool{}
	for _, e := range s.Enums {
		enumCols[e.Column] = true
	}
	if !enumCols["publisher.publisher_name"] {
		t.Fatalf("enums miss publisher.publisher_name: %+v", s.Enums)
	}
	if len(s.Paths) != 1 {
		t.Fatalf("paths = %d, want 1", len(s.Paths))
	}
	if len(s.Synonyms) != 2 || len(s.Conventions) == 0 || len(s.Gotchas) == 0 || len(s.Examples) == 0 {
		t.Fatalf("synonyms=%d conventions=%d gotchas=%d examples=%d",
			len(s.Synonyms), len(s.Conventions), len(s.Gotchas), len(s.Examples))
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("generated layer fails validation: %s", err)
	}
}

func TestGenerateSQLitePrimaryKeyFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nopk.sqlite")
	sqldb, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	for _, stmt := range []string{
		"CREATE TABLE a (code TEXT, val INTEGER)",
		"INSERT INTO a VALUES ('x', 1)",
		"CREATE TABLE b (id INTEGER PRIMARY KEY, a_code TEXT REFERENCES a(code))",
		"INSERT INTO b VALUES (1, 'x')",
	} {
		if _, err := sqldb.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	s, err := GenerateSQLite(sqldb, GenOptions{Dataset: "nopk"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range s.Tables {
		if tbl.Name == "a" && tbl.PrimaryKey != "val" {
			t.Fatalf("table a primary key = %q, want val", tbl.PrimaryKey)
		}
	}
}

func TestGenerateSQLiteNoTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sqlite")
	sqldb, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := sqldb.Exec("CREATE TABLE t (id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqldb.Exec("DROP TABLE t"); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateSQLite(sqldb, GenOptions{}); err == nil {
		t.Fatal("empty database: want error")
	}
}

func TestGenerateSQLiteNoForeignKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nofk.sqlite")
	sqldb, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := sqldb.Exec("CREATE TABLE solo (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateSQLite(sqldb, GenOptions{}); err == nil {
		t.Fatal("database without foreign keys: want error")
	}
}

func TestGenerateSQLiteHighCardinalitySkipsEnum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wide.sqlite")
	sqldb, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := sqldb.Exec("CREATE TABLE dim (id INTEGER PRIMARY KEY, tag TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqldb.Exec("CREATE TABLE fact (id INTEGER PRIMARY KEY, dim_id INTEGER REFERENCES dim(id), serial INTEGER)"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqldb.Exec("INSERT INTO dim VALUES (1, 'one')"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxGenEnumValues+5; i++ {
		if _, err := sqldb.Exec("INSERT INTO fact (id, dim_id, serial) VALUES (?, 1, ?)", i+1, 1000+i); err != nil {
			t.Fatal(err)
		}
	}
	s, err := GenerateSQLite(sqldb, GenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range s.Enums {
		if e.Column == "fact.serial" {
			t.Fatalf("high-cardinality column got an enum: %+v", e)
		}
	}
}

func TestGenerateSQLiteYAMLRoundTrip(t *testing.T) {
	s, err := GenerateSQLite(openGenTestDB(t), GenOptions{Dataset: "test db", Title: "the test database"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.ToYAML()
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(data)
	if err != nil {
		t.Fatalf("generated YAML does not parse: %s", err)
	}
	if len(back.Tables) != len(s.Tables) || len(back.Enums) != len(s.Enums) || len(back.Paths) != len(s.Paths) {
		t.Fatal("round trip loses tables, enums or paths")
	}
}

func TestRenderTitle(t *testing.T) {
	s, err := GenerateSQLite(openGenTestDB(t), GenOptions{Dataset: "test db", Title: "the test database"})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Render(); !strings.HasPrefix(got, "You answer questions about the test database by writing SQL.") {
		t.Fatalf("rendered header = %q", strings.SplitN(got, "\n", 2)[0])
	}
	s.Title = ""
	if got := s.Render(); !strings.HasPrefix(got, "You answer questions about an ecommerce database by writing SQL.") {
		t.Fatalf("legacy header = %q", strings.SplitN(got, "\n", 2)[0])
	}
}
