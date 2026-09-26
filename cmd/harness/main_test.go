package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/semantic"
)

func TestParseSemanticRenderArgs(t *testing.T) {
	levels, err := parseSemanticRenderArgs(nil)
	if err != nil || !levels.IsAll() {
		t.Fatalf("default = %v %v", levels, err)
	}
	levels, err = parseSemanticRenderArgs([]string{"--context-levels", "model"})
	if err != nil || levels != (semantic.Levels{Model: true}) {
		t.Fatalf("model = %v %v", levels, err)
	}
	_, err = parseSemanticRenderArgs([]string{"--bogus", "value"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag --bogus") {
		t.Fatalf("unknown flag = %v, want it to name --bogus", err)
	}
	if _, err := parseSemanticRenderArgs([]string{"context-levels", "model"}); err == nil {
		t.Fatal("non-dash argument: want error")
	}
}

func TestGuardRejected(t *testing.T) {
	stmt := "UPDATE users SET email = 'x' WHERE id = 1"
	_, verr := db.Query(t.Context(), nil, stmt, 10)
	if !guardRejected(stmt, verr) {
		t.Fatalf("validation error %v: want guard", verr)
	}
	readOnly := fmt.Errorf("exec: %w", &pgconn.PgError{Code: "25006", Message: "cannot execute UPDATE in a read-only transaction"})
	if !guardRejected(stmt, readOnly) {
		t.Fatal("25006: want guard")
	}
	if guardRejected(stmt, &pgconn.PgError{Code: "42P01"}) {
		t.Fatal("undefined table: not the guard")
	}
	if guardRejected(stmt, errors.New("dial tcp: connection refused")) {
		t.Fatal("connection error: not the guard")
	}
	if guardRejected(stmt, nil) {
		t.Fatal("nil: not the guard")
	}
}

func TestRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bench"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bench", "questions.yaml"), []byte("[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "cmd", "harness")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := repoRoot(sub); got != root {
		t.Fatalf("from subdir = %q, want %q", got, root)
	}
	if got := repoRoot(root); got != root {
		t.Fatalf("from root = %q, want %q", got, root)
	}
	if got := repoRoot(t.TempDir()); got != "" {
		t.Fatalf("outside = %q, want empty", got)
	}
	t.Chdir(sub)
	if err := chdirRepoRoot(); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(root); cwd != root && cwd != want {
		t.Fatalf("cwd after chdir = %q, want %q", cwd, root)
	}
}
