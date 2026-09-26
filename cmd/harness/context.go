package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

const (
	contextUsage         = `usage: harness context diagnose --run FILE|CONFIG|latest.json [--strict] | harness context author --run FILE|CONFIG|latest.json [--strict] [--provider anthropic|venice] [--tier cheap|mid|top] [--out DIR] | harness context verify --before FILE --only ID[,ID...] [--repeat N] [--tier cheap|mid|top] [--provider anthropic|venice]`
	contextDiagnoseUsage = `usage: harness context diagnose --run FILE|CONFIG|latest.json [--strict]`
	contextAuthorUsage   = `usage: harness context author --run FILE|CONFIG|latest.json [--strict] [--provider anthropic|venice] [--tier cheap|mid|top] [--out DIR]`
	contextVerifyUsage   = `usage: harness context verify --before FILE --only ID[,ID...] [--repeat N] [--tier cheap|mid|top] [--provider anthropic|venice]`
	contextDraftsDir     = "local/context-drafts"
)

func contextCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", contextUsage)
	}
	switch args[0] {
	case "diagnose":
		return contextDiagnose(args[1:])
	case "author":
		return contextAuthor(args[1:])
	case "verify":
		return contextVerify(args[1:])
	default:
		return fmt.Errorf("unknown context command %q\n%s", args[0], contextUsage)
	}
}

type diagnoseArgs struct {
	run    string
	strict bool
}

func parseDiagnoseArgs(args []string) (diagnoseArgs, error) {
	var out diagnoseArgs
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !strings.HasPrefix(args[i], "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", args[i], contextDiagnoseUsage)
		}
		if !hasValue && name == "strict" {
			out.strict = true
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", args[i], contextDiagnoseUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "run":
			out.run = value
		case "strict":
			if value != "true" && value != "false" {
				return out, fmt.Errorf("--strict must be true or false")
			}
			out.strict = value == "true"
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", args[i], contextDiagnoseUsage)
		}
	}
	if out.run == "" {
		return out, fmt.Errorf("missing --run\n%s", contextDiagnoseUsage)
	}
	return out, nil
}

func resolveContextRun(arg string) (string, error) {
	if strings.HasSuffix(arg, ".jsonl") || strings.HasSuffix(arg, ".json") {
		return resolveRunFile(arg, "")
	}
	provider, err := defaultProvider("")
	if err != nil {
		return "", err
	}
	return resolveRunFile(arg, provider)
}

func loadRunQuestions() ([]bench.Question, map[string]bench.Question, error) {
	qs, err := loadQuestions()
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]bench.Question, len(qs))
	for _, q := range qs {
		byID[q.ID] = q
	}
	return qs, byID, nil
}

func contextDiagnose(args []string) error {
	opts, err := parseDiagnoseArgs(args)
	if err != nil {
		return err
	}
	opts.run, err = resolveContextRun(opts.run)
	if err != nil {
		return err
	}
	lines, err := bench.ReadLines(opts.run)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("%s has no result lines", opts.run)
	}
	_, byID, err := loadRunQuestions()
	if err != nil {
		return err
	}
	out, err := bench.Diagnose(lines, byID, opts.strict)
	if err != nil {
		return err
	}
	fmt.Printf("diagnose run=%s total=%d wrong=%d strict_only=%d stale_skipped=%d strict=%t\n",
		opts.run, out.Total, out.Wrong, out.StrictOnly, len(out.Stale), opts.strict)
	for _, d := range out.Lines {
		fmt.Print(d.Format())
	}
	for _, s := range out.Stale {
		fmt.Printf("stale: %s rep=%d: %s\n", s.ID, s.Repeat, s.Note)
	}
	if len(out.Lines) == 0 {
		fmt.Println("no wrong lines; nothing to author")
		return nil
	}
	counts := map[string]int{}
	for _, d := range out.Lines {
		counts[d.Class]++
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	fmt.Printf("classifications: %s\n", strings.Join(parts, " "))
	return nil
}

type authorArgs struct {
	run      string
	strict   bool
	provider string
	tier     llm.Tier
	out      string
}

func parseAuthorArgs(args []string) (authorArgs, error) {
	out := authorArgs{provider: llm.ProviderVenice, tier: llm.TierCheap, out: contextDraftsDir}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !strings.HasPrefix(args[i], "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", args[i], contextAuthorUsage)
		}
		if !hasValue && name == "strict" {
			out.strict = true
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", args[i], contextAuthorUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "run":
			out.run = value
		case "strict":
			if value != "true" && value != "false" {
				return out, fmt.Errorf("--strict must be true or false")
			}
			out.strict = value == "true"
		case "provider":
			if _, err := llm.ParseProvider(value); err != nil {
				return out, err
			}
			out.provider = value
		case "tier":
			t, err := llm.ParseTier(value)
			if err != nil {
				return out, err
			}
			out.tier = t
		case "out":
			if strings.TrimSpace(value) == "" {
				return out, fmt.Errorf("--out needs a directory")
			}
			out.out = value
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", args[i], contextAuthorUsage)
		}
	}
	if out.run == "" {
		return out, fmt.Errorf("missing --run\n%s", contextAuthorUsage)
	}
	return out, nil
}

func draftsOutsideSemantic(outDir string) error {
	semPath, err := semantic.DefaultPath()
	if err != nil {
		return err
	}
	semDir, err := filepath.Abs(filepath.Dir(semPath))
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	if abs == semDir || strings.HasPrefix(abs, semDir+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to write drafts inside %s; drafts need explicit human review before they touch the live layer", semDir)
	}
	return nil
}

func messageText(msg *anthropic.Message) string {
	if msg == nil {
		return ""
	}
	var parts []string
	for _, b := range msg.Content {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

var authorSystem = []anthropic.TextBlockParam{{Text: "You draft AI context additions for an ecommerce analytics agent. Reply with exactly one YAML mapping and no other text."}}

func contextAuthor(args []string) error {
	opts, err := parseAuthorArgs(args)
	if err != nil {
		return err
	}
	if err := draftsOutsideSemantic(opts.out); err != nil {
		return err
	}
	opts.run, err = resolveContextRun(opts.run)
	if err != nil {
		return err
	}
	lines, err := bench.ReadLines(opts.run)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("%s has no result lines", opts.run)
	}
	_, byID, err := loadRunQuestions()
	if err != nil {
		return err
	}
	out, err := bench.Diagnose(lines, byID, opts.strict)
	if err != nil {
		return err
	}
	if len(out.Lines) == 0 {
		fmt.Printf("author run=%s: no failures; no drafts written\n", opts.run)
		return nil
	}
	var diags []bench.Diagnosis
	seen := map[string]bool{}
	for _, d := range out.Lines {
		if seen[d.ID] {
			continue
		}
		seen[d.ID] = true
		diags = append(diags, d)
	}
	semPath, err := semantic.DefaultPath()
	if err != nil {
		return err
	}
	sem, err := semantic.Load(semPath)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	client, err := newProvider(cfg, opts.provider)
	if err != nil {
		return err
	}
	timeout := time.Duration(len(diags))*4*time.Minute + 2*time.Minute
	if timeout < 10*time.Minute {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	call := func(ctx context.Context, purpose, prompt string) (bench.AuthorReply, error) {
		msg, rec, err := client.Call(ctx, llm.Request{
			Tier:     opts.tier,
			Purpose:  purpose,
			System:   authorSystem,
			Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
		}, nil)
		if err != nil {
			return bench.AuthorReply{}, err
		}
		return bench.AuthorReply{Text: messageText(msg), Model: rec.Model, Usage: rec.Usage, CostUSD: rec.CostUSD}, nil
	}
	tally, authorErr := authorDrafts(diags, func(d bench.Diagnosis) (bench.Draft, []bench.AuthorReply, error) {
		return bench.AuthorOne(ctx, call, sem, byID[d.ID], d)
	})
	if len(tally.drafts) == 0 && authorErr != nil {
		return fmt.Errorf("%w (spent $%.6f, no drafts written)", authorErr, tally.cost)
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	path := filepath.Join(opts.out, stamp+".yaml")
	names := tally.modelNames()
	f := bench.DraftFile{
		SourceRun: opts.run, CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Provider: opts.provider, Model: strings.Join(names, ","),
		Tier: string(opts.tier), TotalCostUSD: tally.cost, Drafts: tally.drafts,
	}
	if err := bench.WriteDraftFile(path, f); err != nil {
		return err
	}
	for _, d := range tally.drafts {
		fmt.Print(d.Summary())
	}
	fmt.Printf("wrote %s drafts=%d cost=$%.6f prompt_tokens=%d output_tokens=%d model=%s\n",
		path, len(tally.drafts), tally.cost, tally.usage.PromptTokens(), tally.usage.OutputTokens, strings.Join(names, ","))
	fmt.Println("drafts need explicit human review; nothing was merged into semantic/ecomm.yaml")
	if authorErr != nil {
		return fmt.Errorf("%w (wrote %d of %d drafts to %s before failing)", authorErr, len(tally.drafts), len(diags), path)
	}
	return nil
}

type authorTally struct {
	drafts []bench.Draft
	cost   float64
	usage  llm.Usage
	models map[string]bool
}

func (t authorTally) modelNames() []string {
	var names []string
	for m := range t.models {
		names = append(names, m)
	}
	sort.Strings(names)
	return names
}

func authorDrafts(diags []bench.Diagnosis, one func(bench.Diagnosis) (bench.Draft, []bench.AuthorReply, error)) (authorTally, error) {
	t := authorTally{models: map[string]bool{}}
	for _, d := range diags {
		draft, replies, err := one(d)
		for _, r := range replies {
			t.cost += r.CostUSD
			t.usage = t.usage.Add(r.Usage)
			if r.Model != "" {
				t.models[r.Model] = true
			}
		}
		if err != nil {
			return t, fmt.Errorf("authoring %s: %w", d.ID, err)
		}
		t.drafts = append(t.drafts, draft)
		fmt.Printf("drafted for %s: %s (%s level) in %d attempt(s)\n", d.ID, draft.Kind, draft.Level, len(replies))
	}
	return t, nil
}

type verifyArgs struct {
	before   string
	only     string
	tier     llm.Tier
	provider string
	repeat   int
}

func parseVerifyArgs(args []string) (verifyArgs, error) {
	out := verifyArgs{repeat: bench.DefaultRepeat}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !strings.HasPrefix(args[i], "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", args[i], contextVerifyUsage)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", args[i], contextVerifyUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "before":
			out.before = value
		case "only":
			out.only = value
		case "repeat":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return out, fmt.Errorf("--repeat must be a positive integer")
			}
			out.repeat = n
		case "tier":
			t, err := llm.ParseTier(value)
			if err != nil {
				return out, err
			}
			out.tier = t
		case "provider":
			if _, err := llm.ParseProvider(value); err != nil {
				return out, err
			}
			out.provider = value
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", args[i], contextVerifyUsage)
		}
	}
	if out.before == "" {
		return out, fmt.Errorf("missing --before\n%s", contextVerifyUsage)
	}
	if strings.TrimSpace(out.only) == "" {
		return out, fmt.Errorf("missing --only\n%s", contextVerifyUsage)
	}
	return out, nil
}

func mostCommonTier(lines []bench.Line) llm.Tier {
	counts := map[llm.Tier]int{}
	for _, l := range lines {
		if l.Tier != "" {
			counts[l.Tier]++
		}
	}
	best := llm.TierMid
	top := 0
	for _, t := range llm.Tiers {
		if counts[t] > top {
			best, top = t, counts[t]
		}
	}
	return best
}

func mostCommonProvider(lines []bench.Line, fallback string) string {
	counts := map[string]int{}
	for _, l := range lines {
		if l.Provider != "" {
			counts[l.Provider]++
		}
	}
	names := make([]string, 0, len(counts))
	for p := range counts {
		names = append(names, p)
	}
	sort.Strings(names)
	best := fallback
	top := 0
	for _, p := range names {
		if counts[p] > top {
			best, top = p, counts[p]
		}
	}
	return best
}

func verifyTarget(before []bench.Line, opts verifyArgs, fallback string) (llm.Tier, string) {
	tier, provider := opts.tier, opts.provider
	if tier == "" {
		tier = mostCommonTier(before)
	}
	if provider == "" {
		provider = mostCommonProvider(before, fallback)
	}
	return tier, provider
}

const verifyTag = "verify"

func verifyRunArgs(targetConfig, provider, onlySpec string, repeat int) ([]string, []string) {
	target := []string{"--config", targetConfig, "--provider", provider, "--only", onlySpec, "--tag", verifyTag}
	if repeat > 1 {
		target = append(target, "--repeat", strconv.Itoa(repeat))
	}
	after := []string{"--config", "always-mid", "--provider", provider, "--tag", verifyTag}
	return target, after
}

func contextVerify(args []string) error {
	opts, err := parseVerifyArgs(args)
	if err != nil {
		return err
	}
	qs, _, err := loadRunQuestions()
	if err != nil {
		return err
	}
	target, err := selectOnly(qs, opts.only)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	defaultProvider, err := resolveProvider(cfg, opts.provider)
	if err != nil {
		return err
	}
	beforePath, err := resolveRunFile(opts.before, defaultProvider)
	if err != nil {
		return err
	}
	beforeLines, err := bench.ReadLines(beforePath)
	if err != nil {
		return err
	}
	if len(beforeLines) == 0 {
		return fmt.Errorf("%s has no result lines", beforePath)
	}
	tier, provider := verifyTarget(beforeLines, opts, defaultProvider)
	targetConfig := "always-" + string(tier)
	onlyIDs := make([]string, len(target))
	for i, q := range target {
		onlyIDs[i] = q.ID
	}
	onlySpec := strings.Join(onlyIDs, ",")
	targetArgs, afterArgs := verifyRunArgs(targetConfig, provider, onlySpec, opts.repeat)
	fmt.Printf("verify: targeted rerun of %s on %s/%s, then full always-mid on %s tagged %s\n", onlySpec, provider, targetConfig, provider, verifyTag)
	targetOut, err := runBench(targetArgs)
	if err != nil {
		return err
	}
	targetSummary, err := bench.ReadSummary(filepath.Join(targetOut, bench.LatestFileName))
	if err != nil {
		return err
	}
	targetLines, err := bench.ReadLines(targetSummary.Source)
	if err != nil {
		return err
	}
	fmt.Print(formatTargeted(onlyIDs, beforeLines, targetLines))
	afterOut, err := runBench(afterArgs)
	if err != nil {
		return err
	}
	afterSummary, err := bench.ReadSummary(filepath.Join(afterOut, bench.LatestFileName))
	if err != nil {
		return err
	}
	afterLines, err := bench.ReadLines(afterSummary.Source)
	if err != nil {
		return err
	}
	fmt.Print(bench.Compare(bench.NewSide(opts.before, beforePath, beforeLines), bench.NewSide("after", afterSummary.Source, afterLines)).Format())
	return nil
}

type verdictTally struct {
	last   bench.Line
	n      int
	passed int
	strict int
}

func verdictOf(lines []bench.Line, id string) (verdictTally, bool) {
	var t verdictTally
	for _, l := range lines {
		if l.ID != id {
			continue
		}
		t.n++
		t.last = l
		if l.Correct {
			t.passed++
		}
		if l.CorrectStrict {
			t.strict++
		}
	}
	return t, t.n > 0
}

func verdictWord(ok bool) string {
	if ok {
		return "pass"
	}
	return "FAIL"
}

func tallyWord(k, n int) string {
	if n == 1 {
		return verdictWord(k == 1)
	}
	return fmt.Sprintf("%d/%d", k, n)
}

func formatTargeted(ids []string, before, after []bench.Line) string {
	var b strings.Builder
	b.WriteString("targeted rerun:\n")
	for _, id := range ids {
		bl, bok := verdictOf(before, id)
		al, aok := verdictOf(after, id)
		if !bok {
			fmt.Fprintf(&b, "%s: missing from before run\n", id)
			continue
		}
		if !aok {
			fmt.Fprintf(&b, "%s: missing from targeted rerun\n", id)
			continue
		}
		fmt.Fprintf(&b, "%s: before lenient=%s strict=%s answer=%q; after lenient=%s strict=%s answer=%q\n",
			id, tallyWord(bl.passed, bl.n), tallyWord(bl.strict, bl.n), truncate(bl.last.Answer, 60),
			tallyWord(al.passed, al.n), tallyWord(al.strict, al.n), truncate(al.last.Answer, 60))
	}
	return b.String()
}
