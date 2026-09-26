package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, dir string, body string) string {
	t.Helper()
	path := filepath.Join(dir, EnvFileName)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFindEnvFileInCurrentDir(t *testing.T) {
	t.Setenv(EnvFileOverride, "")
	dir := t.TempDir()
	want := writeEnv(t, dir, "DATABASE_URL=x\n")
	if got := FindEnvFileIn(dir); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindEnvFileInOverrideFile(t *testing.T) {
	dir := t.TempDir()
	fileDir := t.TempDir()
	custom := filepath.Join(fileDir, "custom.env")
	if err := os.WriteFile(custom, []byte("DATABASE_URL=x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvFileOverride, custom)
	if got := FindEnvFileIn(dir); got != custom {
		t.Fatalf("got %q, want %q", got, custom)
	}
}

func TestFindEnvFileInOverrideDir(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	want := writeEnv(t, other, "DATABASE_URL=x\n")
	t.Setenv(EnvFileOverride, other)
	if got := FindEnvFileIn(dir); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindEnvFileInPrefersCurrentDir(t *testing.T) {
	dir := t.TempDir()
	want := writeEnv(t, dir, "DATABASE_URL=fromdir\n")
	other := t.TempDir()
	writeEnv(t, other, "DATABASE_URL=fromoverride\n")
	t.Setenv(EnvFileOverride, other)
	if got := FindEnvFileIn(dir); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFindEnvFileInNone(t *testing.T) {
	t.Setenv(EnvFileOverride, "")
	dir := t.TempDir()
	if got := FindEnvFileIn(dir); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestLoadInReadsFile(t *testing.T) {
	t.Setenv(EnvFileOverride, "")
	for _, k := range []string{AnthropicAPIKeyName, VeniceAPIKeyName, JevAPIKeyName, DatabaseURLName, AnthropicWorkspace} {
		if v, ok := os.LookupEnv(k); ok {
			t.Cleanup(func() { os.Setenv(k, v) })
		} else {
			t.Cleanup(func() { os.Unsetenv(k) })
		}
		os.Unsetenv(k)
	}
	dir := t.TempDir()
	writeEnv(t, dir, "ANTHROPIC_API_KEY=a\nVENICE_API_KEY=v\nJEV_API_KEY=j\nDATABASE_URL=d\nANTHROPIC_WORKSPACE_ID=w\n")
	cfg, err := LoadIn(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AnthropicAPIKey != "a" || cfg.VeniceAPIKey != "v" || cfg.JevAPIKey != "j" || cfg.DatabaseURL != "d" || cfg.AnthropicWorkspaceID != "w" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestMissingReportsNames(t *testing.T) {
	cfg := Config{DatabaseURL: "d"}
	missing := cfg.Missing()
	if len(missing) != 2 || missing[0] != AnthropicAPIKeyName || missing[1] != VeniceAPIKeyName {
		t.Fatalf("unexpected missing list: %v", missing)
	}
}
