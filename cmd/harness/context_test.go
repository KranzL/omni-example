package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/bench/judge"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

func TestParseDiagnoseArgs(t *testing.T) {
	args, err := parseDiagnoseArgs([]string{"--run", "run.jsonl"})
	if err != nil || args.run != "run.jsonl" || args.strict {
		t.Fatalf("default = %+v, %v", args, err)
	}
	args, err = parseDiagnoseArgs([]string{"--run=run.jsonl", "--strict"})
	if err != nil || !args.strict {
		t.Fatalf("strict = %+v, %v", args, err)
	}
	if _, err := parseDiagnoseArgs([]string{}); err == nil {
		t.Fatal("missing run: want error")
	}
	if _, err := parseDiagnoseArgs([]string{"--run", "r", "--strict=maybe"}); err == nil {
		t.Fatal("bad strict: want error")
	}
	if _, err := parseDiagnoseArgs([]string{"--run", "r", "--other", "x"}); err == nil {
		t.Fatal("unknown flag: want error")
	}
	if !strings.Contains(contextDiagnoseUsage, "--strict") {
		t.Fatalf("usage %q does not mention --strict", contextDiagnoseUsage)
	}
}

func TestParseAuthorArgs(t *testing.T) {
	args, err := parseAuthorArgs([]string{"--run", "run.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if args.provider != llm.ProviderVenice || args.tier != llm.TierCheap || args.out != contextDraftsDir || args.strict {
		t.Fatalf("defaults = %+v", args)
	}
	args, err = parseAuthorArgs([]string{"--run", "r", "--strict", "--provider", "anthropic", "--tier", "mid", "--out", "tmp"})
	if err != nil || args.provider != "anthropic" || args.tier != llm.TierMid || args.out != "tmp" || !args.strict {
		t.Fatalf("overrides = %+v, %v", args, err)
	}
	if _, err := parseAuthorArgs([]string{}); err == nil {
		t.Fatal("missing run: want error")
	}
	if _, err := parseAuthorArgs([]string{"--run", "r", "--provider", "nope"}); err == nil {
		t.Fatal("bad provider: want error")
	}
	if _, err := parseAuthorArgs([]string{"--run", "r", "--tier", "nope"}); err == nil {
		t.Fatal("bad tier: want error")
	}
}

func TestParseVerifyArgs(t *testing.T) {
	args, err := parseVerifyArgs([]string{"--before", "b.jsonl", "--only", "m01,m02"})
	if err != nil || args.before != "b.jsonl" || args.only != "m01,m02" {
		t.Fatalf("parsed = %+v, %v", args, err)
	}
	if _, err := parseVerifyArgs([]string{"--only", "m01"}); err == nil {
		t.Fatal("missing before: want error")
	}
	if _, err := parseVerifyArgs([]string{"--before", "b"}); err == nil {
		t.Fatal("missing only: want error")
	}
	if _, err := parseVerifyArgs([]string{"--before", "b", "--only", "m01", "--tier", "nope"}); err == nil {
		t.Fatal("bad tier: want error")
	}
	if !strings.Contains(contextVerifyUsage, "--only") {
		t.Fatalf("usage %q does not mention --only", contextVerifyUsage)
	}
}

func TestContextCmdUnknown(t *testing.T) {
	if err := contextCmd([]string{"merge"}); err == nil {
		t.Fatal("unknown command: want error")
	}
	if err := contextCmd([]string{}); err == nil {
		t.Fatal("empty command: want error")
	}
}

func TestSelectOnly(t *testing.T) {
	qs := []bench.Question{{ID: "m01"}, {ID: "m02"}, {ID: "h01"}}
	sel, err := selectOnly(qs, "m02")
	if err != nil || len(sel) != 1 || sel[0].ID != "m02" {
		t.Fatalf("single = %+v, %v", sel, err)
	}
	sel, err = selectOnly(qs, "h01, m01,m01")
	if err != nil || len(sel) != 2 || sel[0].ID != "h01" || sel[1].ID != "m01" {
		t.Fatalf("multi = %+v, %v", sel, err)
	}
	if _, err := selectOnly(qs, "m01,zzz"); err == nil || !strings.Contains(err.Error(), "zzz") {
		t.Fatalf("unknown = %v, want error naming zzz", err)
	}
	if _, err := selectOnly(qs, " , "); err == nil {
		t.Fatal("empty spec: want error")
	}
}

func TestBenchRunOutDir(t *testing.T) {
	base := benchRunArgs{config: "always-mid", set: bench.SetFull, levels: semantic.AllLevels()}
	with := func(f func(*benchRunArgs)) benchRunArgs {
		o := base
		f(&o)
		return o
	}
	model, _ := semantic.ParseLevels("model")
	cases := []struct {
		provider string
		opts     benchRunArgs
		want     string
	}{
		{"anthropic", base, filepath.Join("results", "always-mid")},
		{"anthropic", with(func(o *benchRunArgs) { o.tag = "verify" }), filepath.Join("results", "always-mid-verify")},
		{"venice", with(func(o *benchRunArgs) { o.tag = "verify" }), filepath.Join("results", "venice", "always-mid-verify")},
		{"anthropic", with(func(o *benchRunArgs) { o.levels, o.tag = model, "verify" }), filepath.Join("results", "always-mid-levels-model-verify")},
		{"anthropic", with(func(o *benchRunArgs) { o.only = "m01" }), filepath.Join("results", "always-mid-subset")},
		{"anthropic", with(func(o *benchRunArgs) { o.difficulty = []string{"hard"} }), filepath.Join("results", "always-mid-subset")},
		{"anthropic", with(func(o *benchRunArgs) { o.shadow = 0.25 }), filepath.Join("results", "always-mid-shadow")},
		{"anthropic", with(func(o *benchRunArgs) { o.only, o.shadow, o.tag, o.noCache = "m01", 0.5, "verify", true }), filepath.Join("results", "always-mid-nocache-subset-shadow-verify")},
	}
	for _, c := range cases {
		if got := benchRunOutDir(c.provider, c.opts); got != c.want {
			t.Errorf("%+v: got %q want %q", c.opts, got, c.want)
		}
	}
}

func TestVerifyRunArgsTagBothRuns(t *testing.T) {
	target, after := verifyRunArgs("always-cheap", "venice", "m01,m02", 1)
	for _, args := range [][]string{target, after} {
		opts, err := parseBenchRunArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		if opts.tag != verifyTag || opts.provider != "venice" {
			t.Errorf("%v: tag=%q provider=%q, want tag %q on venice", args, opts.tag, opts.provider, verifyTag)
		}
		if benchRunOutDir(opts.provider, opts) == bench.ConfigDir(opts.provider, opts.config) {
			t.Errorf("%v writes into the baseline folder", args)
		}
	}
	if benchRunOutDir("venice", mustParseRun(t, target)) == benchRunOutDir("venice", mustParseRun(t, after)) {
		t.Error("targeted and after runs share a folder")
	}
}

func mustParseRun(t *testing.T, args []string) benchRunArgs {
	t.Helper()
	opts, err := parseBenchRunArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	return opts
}

func TestMostCommonTiesAreStable(t *testing.T) {
	lines := []bench.Line{
		{Tier: llm.TierTop, Provider: "venice"},
		{Tier: llm.TierCheap, Provider: "anthropic"},
		{Tier: llm.TierMid, Provider: "venice"},
		{Tier: llm.TierTop, Provider: "anthropic"},
		{Tier: llm.TierCheap},
		{Tier: llm.TierMid},
	}
	for i := 0; i < 50; i++ {
		if got := mostCommonTier(lines); got != llm.TierCheap {
			t.Fatalf("tier tie = %s, want cheap", got)
		}
		if got := mostCommonProvider(lines, "venice"); got != "anthropic" {
			t.Fatalf("provider tie = %s, want anthropic", got)
		}
	}
	if got := mostCommonProvider(nil, "venice"); got != "venice" {
		t.Fatalf("empty = %s, want fallback venice", got)
	}
}

func TestContextVerifyResolvesBeforeUnderProvider(t *testing.T) {
	err := contextVerify([]string{"--before", "no-such-config", "--only", "e01", "--provider", "venice"})
	if err == nil || !strings.Contains(err.Error(), filepath.Join("results", "venice", "no-such-config")) {
		t.Fatalf("err = %v, want lookup under results/venice", err)
	}
}

func TestAuthorDraftsKeepsDraftsBeforeFailure(t *testing.T) {
	diags := []bench.Diagnosis{{ID: "e01"}, {ID: "e02"}, {ID: "e03"}}
	tally, err := authorDrafts(diags, func(d bench.Diagnosis) (bench.Draft, []bench.AuthorReply, error) {
		reply := []bench.AuthorReply{{Model: "m", CostUSD: 0.5}}
		if d.ID == "e02" {
			return bench.Draft{}, reply, errors.New("boom")
		}
		return bench.Draft{ForQuestion: d.ID}, reply, nil
	})
	if err == nil || !strings.Contains(err.Error(), "e02") {
		t.Fatalf("err = %v, want failure naming e02", err)
	}
	if len(tally.drafts) != 1 || tally.drafts[0].ForQuestion != "e01" {
		t.Fatalf("drafts = %+v, want the e01 draft kept", tally.drafts)
	}
	if tally.cost != 1.0 || len(tally.modelNames()) != 1 {
		t.Fatalf("cost = %v models = %v, want both paid calls counted", tally.cost, tally.modelNames())
	}
}

func TestParseBenchRunTag(t *testing.T) {
	args, err := parseBenchRunArgs([]string{"--config", "always-mid", "--tag", "verify"})
	if err != nil || args.tag != "verify" {
		t.Fatalf("tag = %+v, %v", args, err)
	}
	if _, err := parseBenchRunArgs([]string{"--config", "always-mid", "--tag", "../x"}); err == nil {
		t.Fatal("path tag: want error")
	}
	if _, err := parseBenchRunArgs([]string{"--config", "always-mid", "--tag", "-x"}); err == nil {
		t.Fatal("leading dash tag: want error")
	}
	if !strings.Contains(benchRunUsage, "--tag") || !strings.Contains(benchRunUsage, "--only ID[,ID...]") {
		t.Fatalf("usage %q does not mention --tag and --only list", benchRunUsage)
	}
}

func TestDraftsOutsideSemantic(t *testing.T) {
	if err := draftsOutsideSemantic(t.TempDir()); err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	semPath, err := semantic.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := draftsOutsideSemantic(filepath.Dir(semPath)); err == nil {
		t.Fatal("semantic dir: want refusal")
	}
	if err := draftsOutsideSemantic(filepath.Join(filepath.Dir(semPath), "drafts")); err == nil {
		t.Fatal("semantic subdir: want refusal")
	}
}

func TestVerifyTarget(t *testing.T) {
	lines := []bench.Line{
		{ID: "m01", Tier: llm.TierMid, Provider: "anthropic"},
		{ID: "m02", Tier: llm.TierMid, Provider: "anthropic"},
	}
	tier, provider := verifyTarget(lines, verifyArgs{}, "venice")
	if tier != llm.TierMid || provider != "anthropic" {
		t.Fatalf("defaults = %s %s", tier, provider)
	}
	tier, provider = verifyTarget(lines, verifyArgs{tier: llm.TierCheap, provider: "venice"}, "anthropic")
	if tier != llm.TierCheap || provider != "venice" {
		t.Fatalf("overrides = %s %s", tier, provider)
	}
	tier, provider = verifyTarget([]bench.Line{{ID: "m01"}}, verifyArgs{}, "venice")
	if tier != llm.TierMid || provider != "venice" {
		t.Fatalf("empty = %s %s", tier, provider)
	}
}

func TestFormatTargeted(t *testing.T) {
	before := []bench.Line{{ID: "m01", Correct: false, CorrectStrict: false, Answer: "wrong"}}
	after := []bench.Line{{ID: "m01", Correct: true, CorrectStrict: true, Answer: "right"}}
	text := formatTargeted([]string{"m01", "zzz"}, before, after)
	if !strings.Contains(text, "m01: before lenient=FAIL") || !strings.Contains(text, "after lenient=pass") {
		t.Fatalf("text =\n%s", text)
	}
	if !strings.Contains(text, "zzz: missing from before run") {
		t.Fatalf("text =\n%s", text)
	}
}

func writeRunFixture(t *testing.T, lines []bench.Line) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := bench.WriteLines(path, lines); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestContextDiagnoseFixture(t *testing.T) {
	path := writeRunFixture(t, []bench.Line{
		{ID: "e01", Difficulty: "easy", Repeat: 1, Config: "always-mid", Tier: llm.TierMid, Correct: true, CorrectStrict: true,
			Answer: "x", Expected: "x", SQL: "SELECT 1", Submitted: true},
	})
	if err := contextDiagnose([]string{"--run", path}); err != nil {
		t.Fatal(err)
	}
}

func TestContextAuthorNoFailures(t *testing.T) {
	path := writeRunFixture(t, []bench.Line{
		{ID: "e01", Difficulty: "easy", Repeat: 1, Config: "always-mid", Tier: llm.TierMid, Correct: true, CorrectStrict: true,
			Answer: "x", Expected: "x", SQL: "SELECT 1", Submitted: true},
	})
	if err := contextAuthor([]string{"--run", path, "--out", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
}

func TestContextVerifyBadOnly(t *testing.T) {
	err := contextVerify([]string{"--before", "results/always-mid/latest.json", "--only", "zzz"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want unknown question error", err)
	}
}

func TestParseBenchJudgeProvider(t *testing.T) {
	args, err := parseBenchJudgeArgs([]string{"--run", "always-mid"})
	if err != nil || args.provider != "" {
		t.Fatalf("default = %+v %v, want empty so the config default applies", args, err)
	}
	args, err = parseBenchJudgeArgs([]string{"--run", "always-mid", "--provider=venice"})
	if err != nil || args.provider != "venice" {
		t.Fatalf("venice = %+v %v", args, err)
	}
	if _, err := parseBenchJudgeArgs([]string{"--run", "always-mid", "--provider", "nope"}); err == nil {
		t.Fatal("bad provider: want error")
	}
	if !strings.Contains(benchJudgeUsage, "--provider") {
		t.Fatalf("usage %q does not mention --provider", benchJudgeUsage)
	}
}

func TestBenchJudgeResolvesUnderProvider(t *testing.T) {
	err := benchJudge([]string{"--run", "no-such-config", "--provider", "venice"})
	if err == nil || !strings.Contains(err.Error(), filepath.Join("results", "venice", "no-such-config")) {
		t.Fatalf("err = %v, want lookup under results/venice", err)
	}
}

func TestFormatTargetedCountsRepeats(t *testing.T) {
	before := []bench.Line{
		{ID: "m01", Repeat: 1, Correct: true, CorrectStrict: true, Answer: "a"},
		{ID: "m01", Repeat: 2, Correct: false, Answer: "b"},
		{ID: "m01", Repeat: 3, Correct: true, Answer: "c"},
	}
	after := []bench.Line{
		{ID: "m01", Repeat: 1, Correct: true, CorrectStrict: true, Answer: "x"},
		{ID: "m01", Repeat: 2, Correct: true, CorrectStrict: true, Answer: "y"},
		{ID: "m01", Repeat: 3, Correct: true, CorrectStrict: false, Answer: "z"},
	}
	text := formatTargeted([]string{"m01"}, before, after)
	if !strings.Contains(text, "before lenient=2/3 strict=1/3") || !strings.Contains(text, "after lenient=3/3 strict=2/3") {
		t.Fatalf("text =\n%s", text)
	}
	opts, err := parseVerifyArgs([]string{"--before", "x.jsonl", "--only", "m01", "--repeat", "3"})
	if err != nil || opts.repeat != 3 {
		t.Fatalf("repeat = %+v %v", opts, err)
	}
	target, _ := verifyRunArgs("always-mid", "anthropic", "m01", opts.repeat)
	if run := mustParseRun(t, target); run.repeat != 3 {
		t.Fatalf("targeted rerun repeat %d", run.repeat)
	}
}

func TestContextDiagnoseAcceptsLatestJSON(t *testing.T) {
	run := writeRunFixture(t, []bench.Line{
		{ID: "e01", Difficulty: "easy", Repeat: 1, Config: "always-mid", Tier: llm.TierMid, Correct: true, CorrectStrict: true,
			Answer: "x", Expected: "x", SQL: "SELECT 1", Submitted: true},
	})
	latest := filepath.Join(filepath.Dir(run), bench.LatestFileName)
	if err := bench.WriteSummary(latest, bench.Summary{Config: "always-mid", Source: run}); err != nil {
		t.Fatal(err)
	}
	if err := contextDiagnose([]string{"--run", latest}); err != nil {
		t.Fatal(err)
	}
	if err := contextAuthor([]string{"--run", latest, "--out", t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	err := contextDiagnose([]string{"--run", "no-such-config"})
	if err == nil || !strings.Contains(err.Error(), "no-such-config") {
		t.Fatalf("config name err = %v, want a lookup of no-such-config", err)
	}
}

func TestJudgeConfigNameAndHealth(t *testing.T) {
	first := bench.Line{Config: "always-mid"}
	if got := judgeConfigName(filepath.Join("results", "always-mid-nocache-levels-model-subset-abl", "S.jsonl"), first); got != "always-mid-nocache-levels-model-subset-abl" {
		t.Fatalf("name %q", got)
	}
	venice := bench.Line{Config: "always-mid", Provider: "venice"}
	if got := judgeConfigName(filepath.Join("results", "venice", "always-mid-nocache", "S.jsonl"), venice); got != "venice-always-mid-nocache" {
		t.Fatalf("venice name %q", got)
	}
	if got := judgeConfigName(filepath.Join("tmp", "run.jsonl"), first); got != "always-mid" {
		t.Fatalf("fallback name %q", got)
	}
	if err := judgeHealth([]judge.Line{{Error: "a"}, {Error: "b"}}); err == nil {
		t.Fatal("every line errored: want error")
	}
	if err := judgeHealth([]judge.Line{{Error: "a"}, {}}); err != nil {
		t.Fatalf("one good line: %v", err)
	}
}
