package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/llm"
)

func resolveRunFile(arg, provider string) (string, error) {
	if strings.HasSuffix(arg, ".jsonl") {
		return arg, nil
	}
	latest := arg
	if !strings.HasSuffix(arg, ".json") {
		latest = bench.LatestPath(provider, arg)
	}
	s, err := bench.ReadSummary(latest)
	if err != nil {
		return "", fmt.Errorf("%s: %w", arg, err)
	}
	if s.Source == "" {
		return "", fmt.Errorf("%s has no source run file", latest)
	}
	return s.Source, nil
}

func loadSide(arg, provider string) (bench.Side, error) {
	path, err := resolveRunFile(arg, provider)
	if err != nil {
		return bench.Side{}, err
	}
	lines, err := bench.ReadLines(path)
	if err != nil {
		return bench.Side{}, err
	}
	if len(lines) == 0 {
		return bench.Side{}, fmt.Errorf("%s has no result lines", path)
	}
	return bench.NewSide(arg, path, lines), nil
}

func parseProviderFlag(args []string) (string, []string, error) {
	provider := ""
	var rest []string
	for i := 0; i < len(args); i++ {
		value, isFlag := strings.CutPrefix(args[i], "--provider=")
		if !isFlag && args[i] == "--provider" {
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--provider needs a value")
			}
			i++
			value, isFlag = args[i], true
		}
		if !isFlag {
			rest = append(rest, args[i])
			continue
		}
		if value == "" {
			return "", nil, fmt.Errorf("--provider needs a value")
		}
		p, err := llm.ParseProvider(value)
		if err != nil {
			return "", nil, err
		}
		provider = p
	}
	return provider, rest, nil
}

func benchCompare(args []string) error {
	flag, rest, err := parseProviderFlag(args)
	if err != nil {
		return err
	}
	provider, err := defaultProvider(flag)
	if err != nil {
		return err
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: harness bench compare A B [--provider P], where A and B are run .jsonl files, latest.json files or config names")
	}
	a, err := loadSide(rest[0], provider)
	if err != nil {
		return err
	}
	b, err := loadSide(rest[1], provider)
	if err != nil {
		return err
	}
	fmt.Print(bench.Compare(a, b).Format())
	return nil
}

func benchMetrics(args []string) error {
	paths := args
	if len(paths) == 0 {
		found, err := filepath.Glob(filepath.Join(bench.ResultsDirName, "*", bench.LatestFileName))
		if err != nil {
			return err
		}
		nested, err := filepath.Glob(filepath.Join(bench.ResultsDirName, "*", "*", bench.LatestFileName))
		if err != nil {
			return err
		}
		paths = append(found, nested...)
	}
	var errs []error
	for _, p := range paths {
		if err := metricsOne(p); err != nil {
			fmt.Printf("%s: skipped: %v\n", p, err)
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("bench metrics: %d of %d summaries failed: %w", len(errs), len(paths), errors.Join(errs...))
	}
	return nil
}

func metricsOne(p string) error {
	s, err := bench.ReadSummary(p)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	lines, err := bench.ReadLines(s.Source)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	s = bench.WithOmni(s, lines)
	raw, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	out, err := bench.AppendOmni(raw, *s.Omni)
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	if err := os.WriteFile(p, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", p, omniLine(s))
	return nil
}

func omniLine(s bench.Summary) string {
	if s.Omni == nil {
		return ""
	}
	m := *s.Omni
	out := fmt.Sprintf("tokens_per_correct=%.0f cost_per_correct=$%.6f median_latency=%dms error_free=%d/%d (%.3f)",
		m.TokensPerCorrect, s.CostPerCorrectUSD, m.MedianLatencyMS, m.ErrorFree, s.Total, m.ErrorFreeRate)
	if m.Consistent != nil {
		out += fmt.Sprintf(" consistency=%d/%d (%.3f) answers_changed=%d answer_texts_changed=%d",
			*m.Consistent, m.Questions, *m.ConsistencyRate, *m.AnswersChanged, *m.AnswerTextsChanged)
	}
	return out
}
