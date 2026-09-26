package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

const askUsage = `usage: harness ask "question" [--tier cheap|mid|top] [--runs N] [--provider anthropic|venice] [--context-levels model,topic,field] [--capture-requests]`

type askArgs struct {
	question string
	tier     llm.Tier
	runs     int
	provider string
	levels   semantic.Levels
	capture  bool
}

func parseAskArgs(args []string) (askArgs, error) {
	out := askArgs{tier: llm.TierCheap, runs: 1, levels: semantic.AllLevels()}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		if !hasValue && name == "capture-requests" {
			out.capture = true
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", arg, askUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "tier":
			t, err := llm.ParseTier(value)
			if err != nil {
				return out, err
			}
			out.tier = t
		case "runs":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return out, fmt.Errorf("--runs must be a positive integer")
			}
			out.runs = n
		case "provider":
			if _, err := llm.ParseProvider(value); err != nil {
				return out, err
			}
			out.provider = value
		case "context-levels":
			levels, err := semantic.ParseLevels(value)
			if err != nil {
				return out, err
			}
			out.levels = levels
		case "capture-requests":
			n, err := strconv.ParseBool(value)
			if err != nil {
				return out, fmt.Errorf("--capture-requests must be true or false")
			}
			out.capture = n
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", arg, askUsage)
		}
	}
	if len(positional) != 1 || strings.TrimSpace(positional[0]) == "" {
		return out, fmt.Errorf("%s", askUsage)
	}
	out.question = positional[0]
	return out, nil
}

func ask(args []string) error {
	opts, err := parseAskArgs(args)
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
	client, err := newProvider(cfg, provider)
	if err != nil {
		return err
	}
	if opts.capture {
		enableCapture(client, "")
		fmt.Printf("capture=%s\n", llm.CaptureDir)
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("missing %s", config.DatabaseURLName)
	}
	path, err := semantic.DefaultPath()
	if err != nil {
		return err
	}
	sem, err := semantic.Load(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(opts.runs)*10*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	a := agent.NewPrefix(client, agent.PoolQuerier{Pool: pool}, sem.RenderLevels(opts.levels))

	stamp := time.Now().UTC().Format("20060102T150405Z")
	name := fmt.Sprintf("ask-%s-%s-%s", provider, opts.tier, stamp)
	if !opts.levels.IsAll() {
		name = fmt.Sprintf("ask-%s-%s-%s-%s", provider, opts.tier, opts.levels.Slug(), stamp)
	}
	fmt.Printf("context_levels=%s\n", opts.levels.String())
	resultPath := strings.TrimSuffix(llm.TracePath(name), ".jsonl") + ".result.json"
	runOne := func(i int) (agent.Result, error) {
		res, err := a.Run(ctx, opts.question, opts.tier)
		if werr := llm.WriteJSONL(llm.TracePath(name), res.Records); werr != nil {
			return res, werr
		}
		if opts.runs > 1 {
			fmt.Printf("== run %d of %d\n", i, opts.runs)
		}
		printResult(res)
		return res, err
	}
	if err := askRuns(opts.runs, runOne, func(results []agent.Result) error {
		return writeAskResults(resultPath, results)
	}); err != nil {
		return err
	}
	fmt.Printf("trace=%s result=%s\n", llm.TracePath(name), resultPath)
	return nil
}

func askRuns(runs int, runOne func(i int) (agent.Result, error), save func([]agent.Result) error) error {
	var results []agent.Result
	for i := 1; i <= runs; i++ {
		res, err := runOne(i)
		if err != nil {
			if len(results) == 0 {
				return err
			}
			if serr := save(results); serr != nil {
				return errors.Join(err, serr)
			}
			return fmt.Errorf("run %d of %d failed after %d completed runs were saved: %w", i, runs, len(results), err)
		}
		results = append(results, res)
	}
	if err := save(results); err != nil {
		return err
	}
	for _, r := range results {
		if r.Submitted {
			return nil
		}
	}
	return fmt.Errorf("the agent did not submit an answer in any of %d run(s)", runs)
}

func writeAskResults(path string, results []agent.Result) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func printResult(res agent.Result) {
	status := "submitted"
	if !res.Submitted {
		status = "FAILED: " + res.Failure
	} else if res.Nudged {
		status = "submitted after nudge"
	}
	fmt.Printf("tier: %s\n", res.Tier)
	fmt.Printf("status: %s\n", status)
	fmt.Printf("answer: %s\n", res.Answer)
	fmt.Printf("confidence: %s\n", res.Confidence)
	fmt.Printf("sql: %s\n", res.SQL)
	fmt.Printf("turns: %d sql_calls: %d sql_errors: %d\n", res.Turns, len(res.Steps), res.SQLErrors)
	u := res.Usage
	fmt.Printf("tokens: input=%d output=%d cache_write=%d cache_read=%d prompt_total=%d\n",
		u.InputTokens, u.OutputTokens, u.CacheCreationInputTokens, u.CacheReadInputTokens, u.PromptTokens())
	for _, r := range res.Records {
		fmt.Printf("  %s %s input=%d output=%d cache_write=%d cache_read=%d cost=$%.6f latency=%dms stop=%s\n",
			r.Purpose, r.Model, r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.CacheCreationInputTokens,
			r.Usage.CacheReadInputTokens, r.CostUSD, r.LatencyMS, r.StopReason)
	}
	fmt.Printf("cost: $%.6f\n", res.CostUSD)
	fmt.Printf("latency: %dms\n", res.WallMS)
}
