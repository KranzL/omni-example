package bench

import (
	"testing"

	"github.com/KranzL/omni-example/internal/db"
)

func TestHasOrderBy(t *testing.T) {
	yes := []string{
		"SELECT a FROM t ORDER BY a",
		"select a from t order by 1 desc",
		"SELECT COUNT(*) FROM t GROUP BY x ORDER BY COUNT(*) DESC LIMIT 5",
	}
	for _, s := range yes {
		if !HasOrderBy(s) {
			t.Fatalf("HasOrderBy(%q) = false, want true", s)
		}
	}
	no := []string{
		"SELECT a FROM t",
		"SELECT border, byline FROM t",
		"SELECT * FROM t ORDER BYX",
		"SELECT name FROM (SELECT name FROM t ORDER BY id DESC LIMIT 3) s",
		"SELECT name, RANK() OVER (ORDER BY speed DESC) FROM t",
		"SELECT 'order by' FROM t",
	}
	for _, s := range no {
		if HasOrderBy(s) {
			t.Fatalf("HasOrderBy(%q) = true, want false", s)
		}
	}
}

func TestExecMatchUnordered(t *testing.T) {
	gold := db.Result{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "x"}, {"2", "y"}, {"2", "y"}}}
	same := db.Result{Columns: []string{"a", "b"}, Rows: [][]string{{"2", "y"}, {"1", "x"}, {"2", "y"}}}
	if !ExecMatch(gold, same, false) {
		t.Fatal("reordered multiset: want match")
	}
	renamed := db.Result{Columns: []string{"c", "d"}, Rows: [][]string{{"2", "y"}, {"1", "x"}, {"2", "y"}}}
	if !ExecMatch(gold, renamed, false) {
		t.Fatal("renamed columns: want match")
	}
	fewer := db.Result{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "x"}, {"2", "y"}}}
	if ExecMatch(gold, fewer, false) {
		t.Fatal("dropped duplicate: want mismatch")
	}
	other := db.Result{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "x"}, {"2", "y"}, {"3", "z"}}}
	if ExecMatch(gold, other, false) {
		t.Fatal("different value: want mismatch")
	}
	narrow := db.Result{Columns: []string{"a"}, Rows: [][]string{{"1"}, {"2"}, {"2"}}}
	if ExecMatch(gold, narrow, false) {
		t.Fatal("different arity: want mismatch")
	}
}

func TestExecMatchOrdered(t *testing.T) {
	gold := db.Result{Columns: []string{"a"}, Rows: [][]string{{"1"}, {"2"}}}
	same := db.Result{Columns: []string{"a"}, Rows: [][]string{{"1"}, {"2"}}}
	if !ExecMatch(gold, same, true) {
		t.Fatal("identical order: want match")
	}
	flipped := db.Result{Columns: []string{"a"}, Rows: [][]string{{"2"}, {"1"}}}
	if ExecMatch(gold, flipped, true) {
		t.Fatal("flipped order: want mismatch")
	}
	if !ExecMatch(gold, flipped, false) {
		t.Fatal("flipped order unordered: want match")
	}
}

func TestExecMatchEmpty(t *testing.T) {
	empty := db.Result{Columns: []string{"a"}, Rows: nil}
	if !ExecMatch(empty, db.Result{Columns: []string{"a"}}, false) {
		t.Fatal("two empty results: want match")
	}
	if ExecMatch(empty, db.Result{Columns: []string{"a"}, Rows: [][]string{{"1"}}}, false) {
		t.Fatal("empty versus one row: want mismatch")
	}
}

func TestExecMatchSeparatorInValue(t *testing.T) {
	gold := db.Result{Columns: []string{"a"}, Rows: [][]string{{"x\x00y"}, {"z"}}}
	same := db.Result{Columns: []string{"a"}, Rows: [][]string{{"z"}, {"x\x00y"}}}
	if !ExecMatch(gold, same, false) {
		t.Fatal("values containing separators: want match")
	}
}
