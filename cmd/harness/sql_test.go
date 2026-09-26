package main

import (
	"os"
	"strings"
	"testing"
)

func TestSQLCmdUsage(t *testing.T) {
	if err := sqlCmd(nil); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("no args = %v, want usage error", err)
	}
	if err := sqlCmd([]string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("two args = %v, want usage error", err)
	}
}

func TestSQLCmdGuardAndQuery(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	if err := sqlCmd([]string{"UPDATE users SET email = 'x' WHERE id = 1"}); err == nil {
		t.Fatal("UPDATE: want guard rejection")
	}
	if err := sqlCmd([]string{"SELECT COUNT(*) FROM users"}); err != nil {
		t.Fatalf("SELECT: %v", err)
	}
}
