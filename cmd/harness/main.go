package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/semantic"
)

func main() {
	if err := chdirRepoRoot(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func repoRoot(start string) string {
	dir := bench.FindBenchDir(start)
	if dir == "" {
		return ""
	}
	return filepath.Dir(dir)
}

func chdirRepoRoot() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := repoRoot(cwd)
	if root == "" || root == cwd {
		return nil
	}
	return os.Chdir(root)
}

func run(args []string) error {
	if len(args) == 1 && args[0] == "db-check" {
		return dbCheck()
	}
	if len(args) >= 1 && args[0] == "models" {
		return models(args[1:])
	}
	if len(args) == 1 && args[0] == "venice-models" {
		return veniceModelsCmd()
	}
	if len(args) >= 2 && args[0] == "semantic" && args[1] == "render" {
		return semanticRender(args[2:])
	}
	if len(args) == 2 && args[0] == "bench" && args[1] == "validate" {
		return benchValidate()
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "run" {
		return benchRun(args[2:])
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "compare" {
		return benchCompare(args[2:])
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "regrade" {
		return benchRegrade(args[2:])
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "regress" {
		return benchRegress(args[2:])
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "drift" {
		return benchDrift(args[2:])
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "metrics" {
		return benchMetrics(args[2:])
	}
	if len(args) >= 2 && args[0] == "bench" && args[1] == "judge" {
		return benchJudge(args[2:])
	}
	if len(args) >= 1 && args[0] == "jev" {
		return jevCmd(args[1:])
	}
	if len(args) >= 1 && args[0] == "bird" {
		return birdCmd(args[1:])
	}
	if len(args) >= 1 && args[0] == "report" {
		return reportCmd(args[1:])
	}
	if len(args) >= 1 && args[0] == "ask" {
		return ask(args[1:])
	}
	if len(args) >= 1 && args[0] == "context" {
		return contextCmd(args[1:])
	}
	if len(args) == 2 && args[0] == "sql" {
		return sqlCmd(args[1:])
	}
	return fmt.Errorf("usage: harness db-check | models [--provider anthropic|venice] | venice-models | semantic render [--context-levels L] | bench validate | bench run --config NAME [--bench ecomm|bird] [--set starter|full] [--repeat N] [--concurrency K] [--only ID[,ID...]] [--provider P] [--context-levels L] [--tag TAG] [--no-evidence] [--capture-requests] | bench compare A B [--provider P] | bench regress --baseline A --candidate B [--max-drop N] [--max-flips M] [--provider P] | bench drift --config NAME [--provider P] | bench regrade [--verdicts] [run.jsonl ...] | bench metrics [latest.json ...] | bench judge --run FILE [--concurrency K] [--only ID] [--provider P] | report | bird gen-layers | bird validate | ask \"question\" --tier cheap|mid|top [--provider P] [--context-levels L] [--capture-requests] | jev compare --set seeds|bench [--difficulty D] [--provider P] | context diagnose --run FILE|CONFIG [--strict] | context author --run FILE|CONFIG [--strict] [--provider P] [--tier T] [--out DIR] | context verify --before FILE --only ID[,ID...] [--repeat N] [--tier T] [--provider P] | sql \"SELECT ...\"")
}

func dbCheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("missing DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	tables := []string{"users", "order_items", "events", "inventory_items", "products", "distribution_centers"}
	for _, table := range tables {
		res, err := db.Query(ctx, pool, "SELECT COUNT(*) FROM "+table, 10)
		if err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		if len(res.Rows) == 0 || len(res.Rows[0]) == 0 {
			return fmt.Errorf("%s: empty count result", table)
		}
		fmt.Printf("%s: %s\n", table, res.Rows[0][0])
	}
	stmt := "UPDATE users SET email = 'x' WHERE id = 1"
	_, err = db.Query(ctx, pool, stmt, 10)
	if err == nil {
		return fmt.Errorf("write guard failed to reject UPDATE")
	}
	if !guardRejected(stmt, err) {
		return fmt.Errorf("UPDATE failed for a reason other than the write guard: %w", err)
	}
	fmt.Printf("write guard rejected UPDATE as expected: %s\n", err)
	return nil
}

const sqlStateReadOnlyTransaction = "25006"

func guardRejected(stmt string, err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == sqlStateReadOnlyTransaction
	}
	verr := db.Validate(stmt)
	return verr != nil && err.Error() == verr.Error()
}

func benchValidate() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := bench.FindBenchDir(cwd)
	if dir == "" {
		return fmt.Errorf("bench/questions.yaml not found")
	}
	qs, err := bench.Load(filepath.Join(dir, bench.QuestionsFile))
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("missing DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	computedAt := time.Now().UTC().Format(time.RFC3339)
	entries := make([]bench.SnapshotEntry, 0, len(qs))
	failed := false
	for _, q := range qs {
		start := time.Now()
		res, err := db.Query(ctx, pool, q.SQL, bench.MaxAnswerRows)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("%s ERROR after %.2fs: %s\n", q.ID, elapsed.Seconds(), err)
			failed = true
			continue
		}
		if res.Truncated {
			fmt.Printf("%s ERROR after %.2fs: answer exceeds %d rows\n", q.ID, elapsed.Seconds(), bench.MaxAnswerRows)
			failed = true
			continue
		}
		if elapsed >= 30*time.Second {
			fmt.Printf("%s ERROR after %.2fs: exceeds 30s budget\n", q.ID, elapsed.Seconds())
			failed = true
			continue
		}
		answer, err := bench.BuildAnswer(q, res)
		if err != nil {
			fmt.Printf("%s ERROR after %.2fs: %s\n", q.ID, elapsed.Seconds(), err)
			failed = true
			continue
		}
		entries = append(entries, bench.SnapshotEntry{ID: q.ID, Answer: answer, ComputedAt: computedAt})
		fmt.Printf("== %s (%s) %.2fs\n%s", q.ID, q.Difficulty, elapsed.Seconds(), bench.FormatAnswer(answer))
	}
	if failed {
		return fmt.Errorf("bench validate failed")
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	out := filepath.Join(dir, bench.AnswersFile)
	if err := os.WriteFile(out, append(data, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("wrote %s with %d answers\n", out, len(entries))
	return nil
}

const semanticRenderUsage = `usage: harness semantic render [--context-levels model,topic,field]`

func parseSemanticRenderArgs(args []string) (semantic.Levels, error) {
	levels := semantic.AllLevels()
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return levels, fmt.Errorf("unexpected argument %s\n%s", arg, semanticRenderUsage)
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if name != "context-levels" {
			return levels, fmt.Errorf("unknown flag %s\n%s", arg, semanticRenderUsage)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return levels, fmt.Errorf("flag %s needs a value\n%s", arg, semanticRenderUsage)
			}
			i++
			value = args[i]
		}
		l, err := semantic.ParseLevels(value)
		if err != nil {
			return levels, err
		}
		levels = l
	}
	return levels, nil
}

func semanticRender(args []string) error {
	levels, err := parseSemanticRenderArgs(args)
	if err != nil {
		return err
	}
	path, err := semantic.DefaultPath()
	if err != nil {
		return err
	}
	s, err := semantic.Load(path)
	if err != nil {
		return err
	}
	text := s.RenderLevels(levels)
	fmt.Print(text)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.AnthropicAPIKey == "" {
		fmt.Printf("\n---\ntokens: ~%d (approximate, ANTHROPIC_API_KEY is not set)\n", semantic.ApproxTokens(text))
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	n, err := semantic.CountTokens(ctx, text, cfg.AnthropicAPIKey, cfg.AnthropicWorkspaceID)
	if err != nil {
		return err
	}
	fmt.Printf("\n---\ntokens: %d (model claude-haiku-4-5, count_tokens endpoint)\n", n)
	return nil
}
