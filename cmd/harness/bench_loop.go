package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/bench"
)

const (
	benchRegressUsage = "usage: harness bench regress --baseline A --candidate B [--max-drop N] [--max-flips M] [--provider P], where A and B are run .jsonl files, latest.json files or config names"
	benchDriftUsage   = "usage: harness bench drift --config NAME [--provider P], or harness bench drift DIR"
)

func loadQuestions() ([]bench.Question, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	dir := bench.FindBenchDir(cwd)
	if dir == "" {
		return nil, fmt.Errorf("bench/questions.yaml not found")
	}
	return bench.Load(filepath.Join(dir, bench.QuestionsFile))
}

func regradeTargets() ([]string, error) {
	var out []string
	err := filepath.WalkDir(bench.ResultsDirName, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "traces" || d.Name() == "jev-shadow" || d.Name() == "judge" || d.Name() == "bird") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func benchRegrade(args []string) error {
	verdicts := false
	var paths []string
	for _, a := range args {
		if a == "--verdicts" || a == "-verdicts" {
			verdicts = true
			continue
		}
		paths = append(paths, a)
	}
	qs, err := loadQuestions()
	if err != nil {
		return err
	}
	if verdicts {
		return benchRegradeVerdicts(qs, paths, time.Now().UTC())
	}
	if len(paths) == 0 {
		if paths, err = regradeTargets(); err != nil {
			return err
		}
	}
	regraded := map[string]bool{}
	totalFail, totalFormat := map[string]int{}, map[string]int{}
	for _, p := range paths {
		out, err := bench.RegradeFile(p, qs)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		regraded[pathKey(p)] = true
		fail, format := bench.CountReasons(out)
		for k, v := range fail {
			totalFail[k] += v
		}
		for k, v := range format {
			totalFormat[k] += v
		}
		fmt.Printf("%s: lines=%d fail_reasons: %s; format_reasons: %s\n", p, len(out), bench.FormatReasonCounts(fail), bench.FormatReasonCounts(format))
	}
	latest, err := filepath.Glob(filepath.Join(bench.ResultsDirName, "*", bench.LatestFileName))
	if err != nil {
		return err
	}
	nested, err := filepath.Glob(filepath.Join(bench.ResultsDirName, "*", "*", bench.LatestFileName))
	if err != nil {
		return err
	}
	matched := map[string]bool{}
	for _, p := range append(latest, nested...) {
		s, err := bench.ReadSummary(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if !regraded[pathKey(s.Source)] {
			continue
		}
		matched[pathKey(s.Source)] = true
		lines, err := bench.ReadLines(s.Source)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		fail, format := bench.CountReasons(lines)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		updated, err := bench.SetSummaryReasons(raw, fail, format)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if err := os.WriteFile(p, updated, 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: summary reason counts updated\n", p)
	}
	reportUnmatched(paths, matched)
	fmt.Printf("regraded %d files; fail_reasons: %s; format_reasons: %s\n", len(paths), bench.FormatReasonCounts(totalFail), bench.FormatReasonCounts(totalFormat))
	return nil
}

func pathKey(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

func reportUnmatched(paths []string, matched map[string]bool) {
	for _, p := range paths {
		if !matched[pathKey(p)] {
			fmt.Printf("%s: no summary references this file; no summary updated\n", p)
		}
	}
}

func summaryFiles() ([]string, error) {
	var out []string
	err := filepath.WalkDir(bench.ResultsDirName, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "traces" || d.Name() == "jev-shadow" || d.Name() == "judge" || d.Name() == "bird") {
			return filepath.SkipDir
		}
		if !d.IsDir() && (d.Name() == bench.LatestFileName || strings.HasSuffix(d.Name(), ".summary.json")) {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

func benchRegradeVerdicts(qs []bench.Question, paths []string, now time.Time) error {
	var err error
	if len(paths) == 0 {
		if paths, err = regradeTargets(); err != nil {
			return err
		}
	}
	at := now.Format(time.RFC3339)
	regraded := map[string][]bench.Line{}
	infos := map[string]bench.RegradeInfo{}
	totalChanged, totalStrict := 0, 0
	for _, p := range paths {
		lines, changes, err := bench.RegradeVerdictsFile(p, qs)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		info := bench.RegradeInfo{At: at, Lines: len(lines), Changes: changes}
		for _, c := range changes {
			if c.Field == "correct" {
				info.VerdictsChanged++
			} else {
				info.StrictChanged++
			}
		}
		totalChanged += info.VerdictsChanged
		totalStrict += info.StrictChanged
		key := pathKey(p)
		regraded[key] = lines
		infos[key] = info
		fmt.Printf("%s: lines=%d correct changed=%d correct_strict changed=%d\n", p, len(lines), info.VerdictsChanged, info.StrictChanged)
		for _, c := range changes {
			fmt.Printf("  %s rep=%d %s %t -> %t\n", c.ID, c.Repeat, c.Field, c.From, c.To)
		}
	}
	summaries, err := summaryFiles()
	if err != nil {
		return err
	}
	matched := map[string]bool{}
	for _, p := range summaries {
		s, err := bench.ReadSummary(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		key := pathKey(s.Source)
		lines, ok := regraded[key]
		if !ok {
			continue
		}
		matched[key] = true
		if s.ByDifficulty == nil {
			continue
		}
		updated := bench.ResummarizeVerdicts(s, lines, infos[key])
		if err := bench.WriteSummary(p, updated); err != nil {
			return err
		}
		fmt.Printf("%s: correct %d -> %d, correct_strict %d -> %d\n", p, s.Correct, updated.Correct, s.CorrectStrict, updated.CorrectStrict)
	}
	reportUnmatched(paths, matched)
	fmt.Printf("regraded verdicts in %d files at %s; correct changed %d, correct_strict changed %d\n", len(paths), at, totalChanged, totalStrict)
	return nil
}

type regressArgs struct {
	baseline  string
	candidate string
	maxDrop   int
	maxFlips  int
	provider  string
}

func parseRegressArgs(args []string) (regressArgs, error) {
	provider, rest, err := parseProviderFlag(args)
	if err != nil {
		return regressArgs{}, err
	}
	out := regressArgs{provider: provider}
	for i := 0; i < len(rest); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(rest[i], "-"), "=")
		if !strings.HasPrefix(rest[i], "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", rest[i], benchRegressUsage)
		}
		if !hasValue {
			if i+1 >= len(rest) {
				return out, fmt.Errorf("flag %s needs a value\n%s", rest[i], benchRegressUsage)
			}
			i++
			value = rest[i]
		}
		switch name {
		case "baseline":
			out.baseline = value
		case "candidate":
			out.candidate = value
		case "max-drop", "max-flips":
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				return out, fmt.Errorf("--%s must be a non-negative integer", name)
			}
			if name == "max-drop" {
				out.maxDrop = n
			} else {
				out.maxFlips = n
			}
		default:
			return out, fmt.Errorf("unknown flag --%s\n%s", name, benchRegressUsage)
		}
	}
	if out.baseline == "" || out.candidate == "" {
		return out, fmt.Errorf("missing --baseline or --candidate\n%s", benchRegressUsage)
	}
	return out, nil
}

func benchRegress(args []string) error {
	opts, err := parseRegressArgs(args)
	if err != nil {
		return err
	}
	provider, err := defaultProvider(opts.provider)
	if err != nil {
		return err
	}
	base, err := loadSide(opts.baseline, provider)
	if err != nil {
		return err
	}
	cand, err := loadSide(opts.candidate, provider)
	if err != nil {
		return err
	}
	if declaredSubset(cand.Path) {
		base = restrictToCandidate(base, cand)
	}
	r := bench.Regress(base, cand, opts.maxDrop, opts.maxFlips)
	fmt.Print(r.Format())
	if f := r.Failures(); len(f) > 0 {
		return fmt.Errorf("regression gate failed: %s", strings.Join(f, "; "))
	}
	return nil
}

func declaredSubset(runPath string) bool {
	dir := filepath.Base(filepath.Dir(runPath))
	return strings.HasSuffix(dir, "-starter") || strings.Contains(dir, "-subset")
}

func restrictToCandidate(base, cand bench.Side) bench.Side {
	ids := map[string]bool{}
	for _, l := range cand.Lines {
		ids[l.ID] = true
	}
	var kept []bench.Line
	for _, l := range base.Lines {
		if ids[l.ID] {
			kept = append(kept, l)
		}
	}
	return bench.NewSide(base.Label, base.Path, kept)
}

func driftDir(args []string, fallback string) (string, error) {
	provider, rest, err := parseProviderFlag(args)
	if err != nil {
		return "", err
	}
	if provider == "" {
		provider = fallback
	}
	if len(rest) == 1 && !strings.HasPrefix(rest[0], "-") {
		return rest[0], nil
	}
	if len(rest) == 2 && rest[0] == "--config" {
		return bench.ConfigDir(provider, rest[1]), nil
	}
	if len(rest) == 1 && strings.HasPrefix(rest[0], "--config=") {
		return bench.ConfigDir(provider, strings.TrimPrefix(rest[0], "--config=")), nil
	}
	return "", fmt.Errorf("%s", benchDriftUsage)
}

func benchDrift(args []string) error {
	fallback, err := defaultProvider("")
	if err != nil {
		return err
	}
	dir, err := driftDir(args, fallback)
	if err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no run files in %s", dir)
	}
	sides := make([]bench.Side, 0, len(paths))
	for _, p := range paths {
		lines, err := bench.ReadLines(p)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if len(lines) == 0 {
			continue
		}
		sides = append(sides, bench.NewSide(p, p, lines))
	}
	fmt.Print(bench.FormatDrift(dir, bench.Drift(sides)))
	return nil
}
