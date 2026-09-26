package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/jev"
	"github.com/KranzL/omni-example/internal/router"
	"github.com/KranzL/omni-example/internal/semantic"
)

type birdSetup struct {
	sems     map[string]*semantic.Semantic
	semTexts map[string]string
	dbs      map[string]*sql.DB
}

func (s *birdSetup) close() {
	for _, sqldb := range s.dbs {
		sqldb.Close()
	}
}

func benchRunBird(opts benchRunArgs) (string, error) {
	if opts.set != bench.SetFull {
		return "", fmt.Errorf("--set %q needs --bench ecomm", opts.set)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	base := bench.FindBenchDir(cwd)
	if base == "" {
		return "", fmt.Errorf("bench/questions.yaml not found")
	}
	dir := filepath.Join(base, bench.BirdDirName)
	qs, err := bench.LoadBird(filepath.Join(dir, bench.BirdQuestionsFile))
	if err != nil {
		return "", err
	}
	entries, err := bench.LoadSnapshot(filepath.Join(dir, bench.BirdAnswersFile))
	if err != nil {
		return "", err
	}
	expected, err := bench.ExpectedByID(entries, qs)
	if err != nil {
		return "", err
	}
	qs, err = selectBirdQuestions(qs, opts)
	if err != nil {
		return "", err
	}
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	provider, err := resolveProvider(cfg, opts.provider)
	if err != nil {
		return "", err
	}
	client, err := newProvider(cfg, provider)
	if err != nil {
		return "", err
	}
	if opts.capture {
		enableCapture(client, "")
	}
	setup, err := openBirdSetup(dir, qs, opts.levels)
	if err != nil {
		return "", err
	}
	defer setup.close()
	jobs := len(qs) * opts.repeat
	waves := (jobs + opts.concurrency - 1) / opts.concurrency
	timeout := time.Duration(waves)*6*time.Minute + 5*time.Minute
	if timeout < 30*time.Minute {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	agentFor := func(q bench.Question) bench.AgentRunner {
		var a *agent.Agent
		querier := db.SQLiteBackend{DB: setup.dbs[q.DbID]}
		if opts.noCache {
			a = agent.NewPrefixSQLiteNoCache(client, querier, setup.semTexts[q.DbID])
		} else {
			a = agent.NewPrefixSQLite(client, querier, setup.semTexts[q.DbID])
		}
		if !opts.noEvidence {
			a.Evidence = q.Evidence
		}
		return a
	}
	r := &bench.Runner{
		Questions:   qs,
		Expected:    expected,
		Config:      opts.config,
		Provider:    provider,
		NoCache:     opts.noCache,
		NoEvidence:  opts.noEvidence,
		Repeat:      opts.repeat,
		Concurrency: opts.concurrency,
		Traces:      bench.NewTraceCollector(),
		EX:          bench.BirdEX{DBs: setup.dbs},
	}
	routerName := opts.config
	popts := birdPromptOptions(opts)
	if router.IsCascade(opts.config) {
		r.AgentFor = agentFor
		if opts.config == router.NameCascadeVerifyJev && cfg.JevAPIKey == "" {
			return "", fmt.Errorf("missing %s", config.JevAPIKeyName)
		}
		r.CascadeFor = func(q bench.Question) bench.Solver {
			querier := db.SQLiteBackend{DB: setup.dbs[q.DbID]}
			c, _ := newCascade(opts.config, agentFor(q), client, setup.semTexts[q.DbID], querier, cfg, opts.shadow, popts, birdVerifierEvidence(opts, q))
			return c
		}
	} else {
		r.AgentFor = agentFor
		var shared router.Router
		if f, ok := router.Baseline(opts.config); ok {
			shared = f
		} else if opts.config == router.NameEmbedding {
			shared, err = newEmbeddingRouter(ctx, cfg, filepath.Join(dir, bench.BirdSeedFile))
			if err != nil {
				return "", err
			}
		} else if opts.config != router.NameHeuristic && opts.config != router.NameHeuristicV2 && opts.config != router.NameClassifier && opts.config != router.NameJevClassifier {
			if err := router.ValidName(opts.config); err != nil {
				return "", err
			}
			return "", fmt.Errorf("config %s is not supported with --bench bird", opts.config)
		}
		if opts.config == router.NameJevClassifier && cfg.JevAPIKey == "" {
			return "", fmt.Errorf("missing %s", config.JevAPIKeyName)
		}
		r.RouterFor = func(q bench.Question) router.Router {
			if shared != nil {
				return shared
			}
			switch opts.config {
			case router.NameHeuristic:
				return router.NewHeuristic(setup.sems[q.DbID], router.DefaultHeuristicParams())
			case router.NameHeuristicV2:
				return router.NewNamedHeuristic(router.NameHeuristicV2, setup.sems[q.DbID], router.HeuristicV2Params())
			case router.NameClassifier:
				return router.NewClassifierWith(client, setup.semTexts[q.DbID], popts)
			case router.NameJevClassifier:
				return router.NewJevRouterFor(jev.NewClient(cfg.JevAPIKey), router.NewClassifierWith(client, setup.semTexts[q.DbID], popts), setup.semTexts[q.DbID], opts.shadow, popts.Domain)
			default:
				return nil
			}
		}
	}
	cache := "on"
	if opts.noCache {
		cache = "off"
	}
	evidence := "on"
	if opts.noEvidence {
		evidence = "off"
	}
	fmt.Printf("bench run provider=%s bench=bird config=%s router=%s questions=%d repeat=%d concurrency=%d cache=%s evidence=%s levels=%s capture=%t timeout=%s\n",
		provider, opts.config, routerName, len(qs), opts.repeat, opts.concurrency, cache, evidence, opts.levels.String(), opts.capture, timeout.Round(time.Minute))
	lines, err := r.Run(ctx)
	if err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	outDir := bench.BirdConfigDir(provider, birdRunName(opts))
	outPath := filepath.Join(outDir, stamp+".jsonl")
	if err := bench.WriteLines(outPath, lines); err != nil {
		return "", err
	}
	if err := runHealth(lines, ctx.Err()); err != nil {
		return "", fmt.Errorf("%w; wrote %s but left %s unchanged", err, outPath, filepath.Join(outDir, bench.LatestFileName))
	}
	summary := bench.Summarize(opts.config, stamp, outPath, opts.repeat, lines)
	summary.Provider = provider
	summary.NoCache = opts.noCache
	summary.NoEvidence = opts.noEvidence
	summary.Tag = opts.tag
	if !opts.levels.IsAll() {
		summary.ContextLevels = opts.levels.String()
	}
	summary = bench.WithOmni(summary, lines)
	latestPath := filepath.Join(outDir, bench.LatestFileName)
	if err := bench.WriteSummary(latestPath, summary); err != nil {
		return "", err
	}
	tracePath := bench.BirdTraceFilePath(provider, birdRunName(opts), false, false, stamp)
	if err := bench.WriteTraceFile(tracePath, r.Traces.Ordered(len(lines))); err != nil {
		return "", err
	}
	for _, l := range lines {
		mark := "ok"
		if !l.Correct {
			mark = "WRONG"
		}
		if l.Error != "" {
			mark = "ERROR"
		}
		ex := "ex=?"
		if l.EXCorrect != nil {
			ex = "ex=false"
			if *l.EXCorrect {
				ex = "ex=true"
			}
		}
		fmt.Printf("%s rep=%d %s %s tier=%s label=%s cost=$%.6f route_cost=$%.6f latency=%dms cache_hit=%.2f answer=%q\n",
			l.ID, l.Repeat, mark, ex, l.Tier, l.RouteLabel, l.CostUSD, l.RouteCostUSD, l.LatencyMS, l.CacheHitRatio, truncate(l.Answer, 80))
		if len(l.Attempts) > 0 {
			fmt.Printf("  cascade: %s\n", truncate(l.RouteReason, 200))
		}
		if l.Error != "" {
			fmt.Printf("  error: %s\n", truncate(l.Error, 200))
		}
	}
	printSummary(summary)
	printShadow(lines)
	fmt.Printf("wrote %s, %s and %s\n", outPath, latestPath, tracePath)
	return outDir, nil
}

func selectBirdQuestions(qs []bench.Question, opts benchRunArgs) ([]bench.Question, error) {
	qs = filterDifficulty(qs, opts.difficulty)
	if len(qs) == 0 {
		return nil, fmt.Errorf("no questions match --difficulty %s", strings.Join(opts.difficulty, ","))
	}
	if opts.only == "" {
		return qs, nil
	}
	return selectOnly(qs, opts.only)
}

func birdRunName(opts benchRunArgs) string {
	name := opts.config
	if opts.noCache {
		name += "-nocache"
	}
	if opts.noEvidence {
		name += "-noevidence"
	}
	if !opts.levels.IsAll() {
		name += "-levels-" + opts.levels.Slug()
	}
	return name + filterSuffix(opts)
}

func birdPromptOptions(opts benchRunArgs) router.PromptOptions {
	return router.PromptOptions{Domain: router.DomainBird, NoCache: opts.noCache}
}

func birdVerifierEvidence(opts benchRunArgs, q bench.Question) string {
	if opts.noEvidence {
		return ""
	}
	return q.Evidence
}

func openBirdSetup(dir string, qs []bench.Question, levels semantic.Levels) (*birdSetup, error) {
	s := &birdSetup{
		sems:     map[string]*semantic.Semantic{},
		semTexts: map[string]string{},
		dbs:      map[string]*sql.DB{},
	}
	seen := map[string]bool{}
	for _, q := range qs {
		if seen[q.DbID] {
			continue
		}
		seen[q.DbID] = true
		sqldb, err := db.OpenSQLite(filepath.Join(dir, bench.BirdDBDir, q.DbID+".sqlite"))
		if err != nil {
			s.close()
			return nil, err
		}
		s.dbs[q.DbID] = sqldb
		sem, err := semantic.Load(filepath.Join(dir, bench.BirdSemanticDir, q.DbID+".yaml"))
		if err != nil {
			s.close()
			return nil, err
		}
		s.sems[q.DbID] = sem
		s.semTexts[q.DbID] = sem.RenderLevels(levels)
	}
	return s, nil
}
