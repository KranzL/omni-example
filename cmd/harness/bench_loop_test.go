package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/bench"
)

func TestParseRegressArgs(t *testing.T) {
	got, err := parseRegressArgs([]string{"--baseline", "always-mid", "--candidate=results/x.jsonl", "--max-drop", "1", "--max-flips=2", "--provider", "venice"})
	if err != nil {
		t.Fatal(err)
	}
	want := regressArgs{baseline: "always-mid", candidate: "results/x.jsonl", maxDrop: 1, maxFlips: 2, provider: "venice"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	got, err = parseRegressArgs([]string{"--baseline", "a", "--candidate", "b"})
	if err != nil || got.maxDrop != 0 || got.maxFlips != 0 {
		t.Fatalf("defaults %+v %v", got, err)
	}
	for _, bad := range [][]string{
		{"--baseline", "a"},
		{"--baseline", "a", "--candidate", "b", "--max-drop", "-1"},
		{"--baseline", "a", "--candidate", "b", "--max-flips", "x"},
		{"--baseline", "a", "--candidate", "b", "--nope", "1"},
		{"a", "b"},
	} {
		if _, err := parseRegressArgs(bad); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestParseBenchRunSet(t *testing.T) {
	args, err := parseBenchRunArgs([]string{"--config", "always-mid"})
	if err != nil || args.set != bench.SetFull {
		t.Fatalf("default set %+v %v", args, err)
	}
	args, err = parseBenchRunArgs([]string{"--config", "always-mid", "--set", "starter"})
	if err != nil || args.set != bench.SetStarter {
		t.Fatalf("starter %+v %v", args, err)
	}
	if !strings.Contains(benchRunUsage, "--set starter|full") {
		t.Fatalf("usage %q does not mention --set", benchRunUsage)
	}
}

func TestSelectSetStarter(t *testing.T) {
	dir := filepath.Join("..", "..", bench.BenchDir)
	qs, err := bench.Load(filepath.Join(dir, bench.QuestionsFile))
	if err != nil {
		t.Fatal(err)
	}
	full, err := selectSet(dir, qs, bench.SetFull)
	if err != nil || len(full) != len(qs) {
		t.Fatalf("full: %d %v", len(full), err)
	}
	starter, err := selectSet(dir, qs, bench.SetStarter)
	if err != nil || len(starter) != 10 {
		t.Fatalf("starter: %d %v", len(starter), err)
	}
	if _, err := selectSet(dir, qs, "missing"); err == nil {
		t.Error("missing set: want error")
	}
}

func TestDriftDir(t *testing.T) {
	cases := map[string][]string{
		filepath.Join("results", "always-mid"):           {"--config", "always-mid"},
		filepath.Join("results", "venice", "always-mid"): {"--config", "always-mid", "--provider", "venice"},
		filepath.Join("results", "venice", "always-top"): {"--config", "always-top", "--provider=venice"},
		filepath.Join("results", "always-cheap-starter"): {"--config=always-cheap-starter"},
		filepath.Join("results", "venice", "heuristic"):  {filepath.Join("results", "venice", "heuristic")},
	}
	for want, args := range cases {
		got, err := driftDir(args, "anthropic")
		if err != nil || got != want {
			t.Errorf("%v: got %q %v, want %q", args, got, err, want)
		}
	}
	got, err := driftDir([]string{"--config", "always-mid"}, "venice")
	if err != nil || got != filepath.Join("results", "venice", "always-mid") {
		t.Errorf("fallback venice: got %q %v", got, err)
	}
	if _, err := driftDir(nil, "anthropic"); err == nil {
		t.Error("no args: want error")
	}
}

func TestParseProviderFlag(t *testing.T) {
	cases := []struct {
		args []string
		want string
		rest int
	}{
		{[]string{"a", "b"}, "", 2},
		{[]string{"a", "--provider", "venice", "b"}, "venice", 2},
		{[]string{"--provider=venice", "a"}, "venice", 1},
		{[]string{"--provider=anthropic"}, "anthropic", 0},
	}
	for _, c := range cases {
		got, rest, err := parseProviderFlag(c.args)
		if err != nil || got != c.want || len(rest) != c.rest {
			t.Errorf("%v: got %q %v %v, want %q with %d rest", c.args, got, rest, err, c.want, c.rest)
		}
	}
	for _, bad := range [][]string{{"--provider", "openai"}, {"--provider=nope"}, {"--provider="}, {"--provider"}} {
		if _, _, err := parseProviderFlag(bad); err == nil {
			t.Errorf("%v: want error", bad)
		}
	}
}

func TestParseRegressArgsProviderDefault(t *testing.T) {
	got, err := parseRegressArgs([]string{"--baseline", "a", "--candidate", "b"})
	if err != nil || got.provider != "" {
		t.Fatalf("no flag = %+v %v, want empty provider so the config default applies", got, err)
	}
	if _, err := parseRegressArgs([]string{"--baseline", "a", "--candidate", "b", "--provider", "nope"}); err == nil {
		t.Fatal("bad provider: want error")
	}
}

func TestRegressDeclaredSubsetComparesSharedQuestions(t *testing.T) {
	if !declaredSubset("results/venice/always-mid-starter/20260101T000000Z.jsonl") || !declaredSubset("results/always-mid-subset-verify/x.jsonl") || declaredSubset("results/always-mid/x.jsonl") {
		t.Fatal("declaredSubset")
	}
	base := bench.NewSide("base", "results/always-mid/a.jsonl", []bench.Line{{ID: "e01", Correct: true, CorrectStrict: true}, {ID: "e02", Correct: true, CorrectStrict: true}})
	cand := bench.NewSide("cand", "results/always-mid-starter/b.jsonl", []bench.Line{{ID: "e01", Correct: true, CorrectStrict: true}})
	r := bench.Regress(restrictToCandidate(base, cand), cand, 0, 0)
	if f := r.Failures(); len(f) != 0 || len(r.Missing) != 0 {
		t.Fatalf("failures %v missing %v", f, r.Missing)
	}
	if f := bench.Regress(base, cand, 0, 0).Failures(); len(f) == 0 {
		t.Fatal("unrestricted partial candidate should fail the gate")
	}
}

func writeRunAndSummary(t *testing.T, dir, config string, lines []bench.Line) (string, string) {
	t.Helper()
	run := filepath.Join(dir, config, "run.jsonl")
	if err := bench.WriteLines(run, lines); err != nil {
		t.Fatal(err)
	}
	latest := filepath.Join(dir, config, bench.LatestFileName)
	if err := bench.WriteSummary(latest, bench.Summarize(config, "S", run, 1, lines)); err != nil {
		t.Fatal(err)
	}
	return run, latest
}

func TestBenchMetricsContinuesPastMissingSource(t *testing.T) {
	dir := t.TempDir()
	line := bench.Line{ID: "e01", Difficulty: "easy", Repeat: 1, Correct: true, CorrectStrict: true, LatencyMS: 5}
	run, broken := writeRunAndSummary(t, dir, "a-broken", []bench.Line{line})
	_, good := writeRunAndSummary(t, dir, "b-good", []bench.Line{line})
	if err := os.Remove(run); err != nil {
		t.Fatal(err)
	}
	err := benchMetrics([]string{broken, good})
	if err == nil || !strings.Contains(err.Error(), "a-broken") {
		t.Fatalf("err = %v, want it to name the broken summary", err)
	}
	s, rerr := bench.ReadSummary(good)
	if rerr != nil || s.Omni == nil {
		t.Fatalf("the good summary after a failing one was not rewritten: %+v %v", s.Omni, rerr)
	}
}

func TestRegradeVerdictsMatchesAbsolutePath(t *testing.T) {
	qs, err := loadQuestions()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	line := bench.Line{ID: qs[0].ID, Difficulty: qs[0].Difficulty, Repeat: 1, Answer: "x", Expected: "x", Correct: true, CorrectStrict: true}
	_, latest := writeRunAndSummary(t, bench.ResultsDirName, "always-mid", []bench.Line{line})
	abs, err := filepath.Abs(filepath.Join(bench.ResultsDirName, "always-mid", "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := benchRegradeVerdicts(qs, []string{abs}, time.Unix(0, 0).UTC()); err != nil {
		t.Fatal(err)
	}
	s, err := bench.ReadSummary(latest)
	if err != nil || s.Regraded == nil {
		t.Fatalf("summary not updated for an absolute run path: %+v %v", s.Regraded, err)
	}
}
