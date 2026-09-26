package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/router"
	"github.com/KranzL/omni-example/internal/semantic"
)

func TestRunHealth(t *testing.T) {
	ok := bench.Line{ID: "e01"}
	bad := bench.Line{ID: "e02", Error: "boom"}
	if err := runHealth([]bench.Line{ok, ok, bad}, nil); err != nil {
		t.Fatalf("one of three errored: %v", err)
	}
	if err := runHealth([]bench.Line{ok, bad}, nil); err != nil {
		t.Fatalf("half errored: %v", err)
	}
	err := runHealth([]bench.Line{bad, bad, ok}, nil)
	if err == nil || !strings.Contains(err.Error(), "2 of 3") {
		t.Fatalf("two of three errored = %v, want error naming 2 of 3", err)
	}
	err = runHealth([]bench.Line{ok}, context.DeadlineExceeded)
	if err == nil || !strings.Contains(err.Error(), "0 of 1") {
		t.Fatalf("deadline = %v, want error", err)
	}
}

func TestTruncateRuneBoundary(t *testing.T) {
	got := truncate("ab\u00e9cd", 3)
	if got != "ab..." {
		t.Fatalf("got %q, want %q", got, "ab...")
	}
	if !utf8.ValidString(truncate("\u65e5\u672c\u8a9e", 4)) {
		t.Fatal("truncate produced invalid UTF-8")
	}
	if got := truncate("line1\nline2", 80); got != "line1 | line2" {
		t.Fatalf("newline = %q", got)
	}
}

func TestParseBenchRunNoCache(t *testing.T) {
	base := []string{"--config", "always-mid"}
	args, err := parseBenchRunArgs(base)
	if err != nil || args.noCache {
		t.Fatalf("default = %+v, %v; want noCache false", args, err)
	}
	args, err = parseBenchRunArgs(append(append([]string{}, base...), "--no-cache"))
	if err != nil || !args.noCache {
		t.Fatalf("bare flag = %+v, %v; want noCache true", args, err)
	}
	args, err = parseBenchRunArgs(append(append([]string{}, base...), "--no-cache=false"))
	if err != nil || args.noCache {
		t.Fatalf("no-cache=false = %+v, %v; want noCache false", args, err)
	}
	if _, err := parseBenchRunArgs(append(append([]string{}, base...), "--no-cache", "maybe")); err == nil {
		t.Fatal("no-cache maybe: want error")
	}
	if !strings.Contains(benchRunUsage, "--no-cache") {
		t.Fatalf("usage %q does not mention --no-cache", benchRunUsage)
	}
}

func TestParseBenchRunContextLevels(t *testing.T) {
	base := []string{"--config", "always-mid"}
	args, err := parseBenchRunArgs(base)
	if err != nil || !args.levels.IsAll() {
		t.Fatalf("default = %+v, %v; want all levels", args, err)
	}
	args, err = parseBenchRunArgs(append(append([]string{}, base...), "--context-levels", "model"))
	if err != nil || args.levels != (semantic.Levels{Model: true}) {
		t.Fatalf("model = %+v, %v; want model only", args, err)
	}
	args, err = parseBenchRunArgs(append(append([]string{}, base...), "--context-levels=model,topic"))
	if err != nil || args.levels != (semantic.Levels{Model: true, Topic: true}) {
		t.Fatalf("model,topic = %+v, %v; want model plus topic", args, err)
	}
	if _, err := parseBenchRunArgs(append(append([]string{}, base...), "--context-levels=foo")); err == nil {
		t.Fatal("bad levels: want error")
	}
	if !strings.Contains(benchRunUsage, "--context-levels") {
		t.Fatalf("usage %q does not mention --context-levels", benchRunUsage)
	}
}

func TestParseBenchRunBird(t *testing.T) {
	args, err := parseBenchRunArgs([]string{"--config", "always-cheap"})
	if err != nil || args.bench != "ecomm" || args.noEvidence {
		t.Fatalf("default = %+v, %v; want bench ecomm without no-evidence", args, err)
	}
	args, err = parseBenchRunArgs([]string{"--config", "always-cheap", "--bench", "bird", "--no-evidence"})
	if err != nil || args.bench != "bird" || !args.noEvidence {
		t.Fatalf("bird = %+v, %v; want bench bird with no-evidence", args, err)
	}
	args, err = parseBenchRunArgs([]string{"--config", "always-cheap", "--bench=bird", "--no-evidence=false"})
	if err != nil || args.bench != "bird" || args.noEvidence {
		t.Fatalf("bird explicit false = %+v, %v; want no-evidence false", args, err)
	}
	if _, err := parseBenchRunArgs([]string{"--config", "always-cheap", "--bench", "tiny"}); err == nil {
		t.Fatal("bad bench: want error")
	}
	if _, err := parseBenchRunArgs([]string{"--config", "always-cheap", "--no-evidence"}); err == nil {
		t.Fatal("no-evidence on ecomm: want error")
	}
	if !strings.Contains(benchRunUsage, "--bench") || !strings.Contains(benchRunUsage, "--no-evidence") {
		t.Fatalf("usage %q does not mention --bench or --no-evidence", benchRunUsage)
	}
}

func TestRunPromptOptionsReachRouterAndVerifier(t *testing.T) {
	rt, err := newRouter(context.Background(), router.NameClassifier, nil, nil, "SEM", config.Config{}, 0, router.PromptOptions{NoCache: true})
	if err != nil {
		t.Fatal(err)
	}
	c, ok := rt.(*router.Classifier)
	if !ok || c.System[1].CacheControl.Type != "" {
		t.Fatalf("no-cache classifier system %+v", c.System)
	}
	rt, _ = newRouter(context.Background(), router.NameClassifier, nil, nil, "SEM", config.Config{}, 0, router.PromptOptions{})
	if rt.(*router.Classifier).System[1].CacheControl.Type == "" {
		t.Fatal("default classifier must keep cache_control")
	}
	cas, err := newCascade(router.NameCascadeVerifyHaiku, nil, nil, "SEM", nil, config.Config{}, 0, birdPromptOptions(benchRunArgs{noCache: true}), "ev")
	if err != nil {
		t.Fatal(err)
	}
	v := cas.Verifier
	if v.System[1].CacheControl.Type != "" || v.Evidence != "ev" || !strings.Contains(v.System[0].Text, "SQLite database") || strings.Contains(v.System[0].Text, "ecommerce") {
		t.Fatalf("bird no-cache verifier %+v evidence %q", v.System, v.Evidence)
	}
	cas, _ = newCascade(router.NameCascadeVerifyHaiku, nil, nil, "SEM", nil, config.Config{}, 0, router.PromptOptions{}, "")
	if cas.Verifier.System[0].Text != router.VerifierInstructions || cas.Verifier.System[1].CacheControl.Type == "" || cas.Verifier.Evidence != "" {
		t.Fatal("ecomm verifier must keep the default prompt and cache_control")
	}
	if p := birdPromptOptions(benchRunArgs{}); p.Domain != router.DomainBird || p.NoCache {
		t.Fatalf("bird options %+v", p)
	}
	q := bench.Question{Evidence: "hint"}
	if birdVerifierEvidence(benchRunArgs{}, q) != "hint" || birdVerifierEvidence(benchRunArgs{noEvidence: true}, q) != "" {
		t.Fatal("bird verifier evidence must follow --no-evidence")
	}
}

func TestTraceNamesMatchOutDir(t *testing.T) {
	opts := mustParseRun(t, []string{"--config", "always-mid", "--no-cache", "--context-levels", "model", "--only", "e01", "--tag", "abl"})
	dir := benchRunOutDir("anthropic", opts)
	trace := benchTracePath("anthropic", opts, "S")
	if filepath.Base(trace) != filepath.Base(dir)+"-S.jsonl" {
		t.Fatalf("trace %s, out dir %s", trace, dir)
	}
	if filepath.Base(benchTracePath("venice", opts, "S")) != "venice-"+filepath.Base(dir)+"-S.jsonl" {
		t.Fatalf("venice trace %s", benchTracePath("venice", opts, "S"))
	}
	bird := mustParseRun(t, []string{"--config", "always-mid", "--bench", "bird", "--no-cache", "--no-evidence", "--context-levels", "model", "--tag", "abl"})
	name := birdRunName(bird)
	if name != "always-mid-nocache-noevidence-levels-"+bird.levels.Slug()+"-abl" {
		t.Fatalf("bird name %s", name)
	}
	if filepath.Base(bench.BirdTraceFilePath("anthropic", name, false, false, "S")) != "bird-"+name+"-S.jsonl" {
		t.Fatal("bird trace name")
	}
}

func TestSelectBirdQuestionsOnlyList(t *testing.T) {
	qs := []bench.Question{{ID: "b01", Difficulty: "simple"}, {ID: "b02", Difficulty: "moderate"}, {ID: "b03", Difficulty: "simple"}}
	got, err := selectBirdQuestions(qs, benchRunArgs{only: "b01,b02"})
	if err != nil || len(got) != 2 || got[0].ID != "b01" || got[1].ID != "b02" {
		t.Fatalf("only list = %+v %v", got, err)
	}
	if _, err := selectBirdQuestions(qs, benchRunArgs{only: "b01,b09"}); err == nil || !strings.Contains(err.Error(), "b09") {
		t.Fatalf("missing id err = %v", err)
	}
	got, err = selectBirdQuestions(qs, benchRunArgs{difficulty: []string{"simple"}, only: "b03"})
	if err != nil || len(got) != 1 || got[0].ID != "b03" {
		t.Fatalf("difficulty plus only = %+v %v", got, err)
	}
}

func TestExLineNamesDenominators(t *testing.T) {
	s := bench.Summary{Total: 10, EXCorrect: 4, EXTotal: 8, EXAccuracy: 0.4}
	got := exLine(s)
	for _, want := range []string{"correct=4", "ex_scored=8", "total=10", "accuracy=0.400 (correct/total)", "scored_accuracy=0.500 (correct/ex_scored)"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q missing %q", got, want)
		}
	}
}

func TestShadowCostFromLines(t *testing.T) {
	lines := []bench.Line{
		{RouteGate: &router.GateInfo{Shadowed: true, ShadowCost: 0.002}},
		{RouteGate: &router.GateInfo{}},
		{Attempts: []bench.Attempt{{VerifyGate: &router.GateInfo{Shadowed: true, ShadowCost: 0.001}}, {}}},
	}
	cost, n := shadowCost(lines)
	if n != 2 || cost < 0.0029 || cost > 0.0031 {
		t.Fatalf("shadow cost %v over %d calls", cost, n)
	}
}
