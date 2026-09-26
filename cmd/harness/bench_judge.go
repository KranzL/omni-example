package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/bench/judge"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/llm"
)

const benchJudgeUsage = `usage: harness bench judge --run FILE [--rubrics PATH] [--questions PATH] [--concurrency K] [--only ID] [--provider anthropic|venice]`

type benchJudgeArgs struct {
	run         string
	rubrics     string
	questions   string
	concurrency int
	only        string
	provider    string
}

func parseBenchJudgeArgs(args []string) (benchJudgeArgs, error) {
	out := benchJudgeArgs{concurrency: bench.DefaultConcurrency}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", arg, benchJudgeUsage)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", arg, benchJudgeUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "run":
			out.run = value
		case "rubrics":
			out.rubrics = value
		case "questions":
			out.questions = value
		case "concurrency":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return out, fmt.Errorf("--concurrency must be a positive integer")
			}
			out.concurrency = n
		case "only":
			out.only = strings.TrimSpace(value)
		case "provider":
			if _, err := llm.ParseProvider(value); err != nil {
				return out, err
			}
			out.provider = value
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", arg, benchJudgeUsage)
		}
	}
	if out.run == "" {
		return out, fmt.Errorf("missing --run\n%s", benchJudgeUsage)
	}
	return out, nil
}

func benchJudge(args []string) error {
	opts, err := parseBenchJudgeArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	provider, err := resolveProvider(cfg, opts.provider)
	if err != nil {
		return err
	}
	runPath, err := resolveRunFile(opts.run, provider)
	if err != nil {
		return err
	}
	lines, err := bench.ReadLines(runPath)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("%s has no result lines", runPath)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := bench.FindBenchDir(cwd)
	if dir == "" {
		return fmt.Errorf("bench/questions.yaml not found")
	}
	questionsPath := opts.questions
	if questionsPath == "" {
		questionsPath = filepath.Join(dir, bench.QuestionsFile)
	}
	qs, err := bench.Load(questionsPath)
	if err != nil {
		return err
	}
	rubricsPath := opts.rubrics
	if rubricsPath == "" {
		rubricsPath = filepath.Join(dir, judge.RubricsFile)
	}
	rubrics, err := judge.LoadRubrics(rubricsPath)
	if err != nil {
		return err
	}
	byID := make(map[string]bench.Question, len(qs))
	for _, q := range qs {
		byID[q.ID] = q
	}
	if opts.only != "" {
		var kept []bench.Line
		for _, l := range lines {
			if l.ID == opts.only {
				kept = append(kept, l)
			}
		}
		if len(kept) == 0 {
			return fmt.Errorf("question %q not found in %s", opts.only, runPath)
		}
		lines = kept
	}
	client, err := newProvider(cfg, provider)
	if err != nil {
		return err
	}
	j := judge.New(client)
	waves := (len(lines) + opts.concurrency - 1) / opts.concurrency
	timeout := time.Duration(waves)*3*time.Minute + 5*time.Minute
	if timeout < 15*time.Minute {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fmt.Printf("bench judge run=%s lines=%d concurrency=%d timeout=%s\n", runPath, len(lines), opts.concurrency, timeout.Round(time.Minute))
	out := make([]judge.Line, len(lines))
	sem := make(chan struct{}, opts.concurrency)
	done := make(chan struct{}, len(lines))
	for i, l := range lines {
		sem <- struct{}{}
		go func(i int, l bench.Line) {
			out[i] = judgeLine(ctx, j, byID, rubrics, l)
			<-sem
			done <- struct{}{}
		}(i, l)
	}
	for range lines {
		<-done
	}
	for _, l := range out {
		mark := "pass"
		if !l.Final {
			mark = "FAIL"
		}
		if l.Error != "" {
			mark = "ERROR"
		}
		super := ""
		if l.Supervised {
			super = " supervised=" + l.SupervisorDecision
		}
		fmt.Printf("%s %s lenient=%v strict=%v judge=%s conf=%s%s cost=$%.6f reason=%q\n",
			l.ID, mark, l.Lenient, l.Strict, l.JudgeVerdict, l.JudgeConfidence, super, l.RouteCostUSD, truncate(l.JudgeReason, 100))
		if l.Error != "" {
			fmt.Printf("  error: %s\n", truncate(l.Error, 200))
		}
	}
	config := judgeConfigName(runPath, lines[0])
	stamp := time.Now().UTC().Format("20060102T150405Z")
	outPath := judge.FilePath(config, stamp)
	if err := judge.WriteLines(outPath, out); err != nil {
		return err
	}
	summary := judge.Summarize(config, runPath, stamp, out)
	summaryPath := judge.SummaryPath(config, stamp)
	if err := judge.WriteSummary(summaryPath, summary); err != nil {
		return err
	}
	fmt.Printf("\n%s", summary.Format())
	fmt.Printf("wrote %s and %s\n", outPath, summaryPath)
	return judgeHealth(out)
}

func judgeHealth(out []judge.Line) error {
	errored := 0
	for _, l := range out {
		if l.Error != "" {
			errored++
		}
	}
	if len(out) > 0 && errored == len(out) {
		return fmt.Errorf("bench judge: every line errored (%d of %d)", errored, len(out))
	}
	return nil
}

func judgeConfigName(runPath string, first bench.Line) string {
	name := first.Config
	if dir := filepath.Base(filepath.Dir(runPath)); strings.HasPrefix(dir, first.Config) {
		name = dir
	}
	if first.Provider != "" && first.Provider != llm.ProviderAnthropic {
		name = first.Provider + "-" + name
	}
	return name
}

func judgeLine(ctx context.Context, j *judge.Judge, byID map[string]bench.Question, rubrics map[string]judge.Rubric, l bench.Line) judge.Line {
	out := judge.Line{
		ID:         l.ID,
		Difficulty: l.Difficulty,
		Repeat:     l.Repeat,
		Config:     l.Config,
		Lenient:    l.Correct,
		Strict:     l.CorrectStrict,
		Answer:     l.Answer,
		SQL:        l.SQL,
		Expected:   l.Expected,
	}
	q, ok := byID[l.ID]
	if !ok {
		out.Error = fmt.Sprintf("question %q not in questions file", l.ID)
		return out
	}
	r, ok := rubrics[l.ID]
	if !ok {
		out.Error = fmt.Sprintf("question %q has no rubric", l.ID)
		return out
	}
	graderCorrect := l.Correct
	res, err := j.JudgeOne(ctx, judge.Input{
		QuestionID:    l.ID,
		Question:      q.Text,
		Rubric:        r,
		Expected:      l.Expected,
		Answer:        l.Answer,
		SQL:           l.SQL,
		GraderCorrect: &graderCorrect,
	})
	out.JudgeVerdict = res.Judgment.Verdict
	out.JudgeConfidence = res.Judgment.Confidence
	out.JudgeReason = res.Judgment.Reason
	out.Supervised = res.Supervised
	out.SupervisorDecision = res.Supervision.Decision
	out.SupervisorReason = res.Supervision.Reason
	out.Final = res.FinalPass
	out.RouteCostUSD = res.CostUSD
	out.RouteTokens = res.Tokens
	out.RouteLatencyMS = res.LatencyMS
	out.Records = res.Records
	if err != nil {
		out.Error = err.Error()
	}
	return out
}
