package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/embed"
	"github.com/KranzL/omni-example/internal/jev"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
	"github.com/KranzL/omni-example/internal/semantic"
)

func newRouter(ctx context.Context, name string, client llm.Provider, sem *semantic.Semantic, semText string, cfg config.Config, shadow float64, popts router.PromptOptions) (router.Router, error) {
	if f, ok := router.Baseline(name); ok {
		return f, nil
	}
	switch name {
	case router.NameHeuristic:
		return router.NewHeuristic(sem, router.DefaultHeuristicParams()), nil
	case router.NameHeuristicV2:
		return router.NewNamedHeuristic(router.NameHeuristicV2, sem, router.HeuristicV2Params()), nil
	case router.NameClassifier:
		return router.NewClassifierWith(client, semText, popts), nil
	case router.NameEmbedding:
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		dir := bench.FindBenchDir(cwd)
		if dir == "" {
			return nil, fmt.Errorf("bench/%s not found", bench.RouterSeedFile)
		}
		return newEmbeddingRouter(ctx, cfg, filepath.Join(dir, bench.RouterSeedFile))
	case router.NameJevClassifier:
		if cfg.JevAPIKey == "" {
			return nil, fmt.Errorf("missing %s", config.JevAPIKeyName)
		}
		return router.NewJevRouterFor(jev.NewClient(cfg.JevAPIKey), router.NewClassifierWith(client, semText, popts), semText, shadow, popts.Domain), nil
	default:
		return nil, router.ValidName(name)
	}
}

func newEmbeddingRouter(ctx context.Context, cfg config.Config, seedPath string) (router.Router, error) {
	if cfg.VeniceAPIKey == "" {
		return nil, fmt.Errorf("missing %s", config.VeniceAPIKeyName)
	}
	seeds, err := router.LoadSeeds(seedPath)
	if err != nil {
		return nil, err
	}
	r := router.NewEmbedding(embed.NewClient(cfg.VeniceAPIKey), llm.VeniceEmbedBgeM3, seeds, router.DefaultEmbeddingParams())
	if err := r.Warm(ctx); err != nil {
		return nil, err
	}
	fmt.Printf("embedding seeds=%d seed_cost=$%.6f seed_http_calls=%d\n", len(seeds), r.SeedCost, r.SeedCalls)
	return r, nil
}

func newCascade(name string, a router.AgentRunner, client llm.Provider, semText string, q agent.Querier, cfg config.Config, shadow float64, popts router.PromptOptions, evidence string) (*router.Cascade, error) {
	var v *router.Verifier
	if tier, ok := router.CascadeVerifierTier(name); ok {
		v = router.NewVerifierWith(client, tier, semText, q, popts)
		v.Evidence = evidence
	}
	if name == router.NameCascadeVerifyJev {
		if cfg.JevAPIKey == "" {
			return nil, fmt.Errorf("missing %s", config.JevAPIKeyName)
		}
		v = router.NewJevGatedVerifier(jev.NewClient(cfg.JevAPIKey), v, semText, shadow)
	}
	return router.NewCascade(name, a, v), nil
}

const benchRunUsage = `usage: harness bench run --config always-cheap|always-mid|always-top|always-muse|heuristic|heuristic-v2|classifier|embedding|jev-classifier|cascade-signals|cascade-verify-haiku|cascade-verify-sonnet|cascade-verify-jev [--bench ecomm|bird] [--set starter|full] [--repeat N] [--concurrency K] [--only ID[,ID...]] [--difficulty easy,moderate,hard] [--shadow RATE] [--provider anthropic|venice|muse] [--context-levels model,topic,field] [--tag TAG] [--no-cache] [--no-evidence] [--capture-requests] [--route-context] [--cheap-thinking BUDGET]`

type benchRunArgs struct {
	config      string
	bench       string
	set         string
	repeat      int
	concurrency int
	only        string
	difficulty  []string
	shadow      float64
	provider    string
	levels      semantic.Levels
	tag         string
	noCache     bool
	noEvidence  bool
	capture     bool
	routeCtx    bool
	cheapThink  int64
}

func parseBenchRunArgs(args []string) (benchRunArgs, error) {
	out := benchRunArgs{bench: bench.BenchEcomm, set: bench.SetFull, repeat: bench.DefaultRepeat, concurrency: bench.DefaultConcurrency, levels: semantic.AllLevels()}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", arg, benchRunUsage)
		}
		if !hasValue && name == "no-cache" {
			out.noCache = true
			continue
		}
		if !hasValue && name == "no-evidence" {
			out.noEvidence = true
			continue
		}
		if !hasValue && name == "capture-requests" {
			out.capture = true
			continue
		}
		if !hasValue && name == "route-context" {
			out.routeCtx = true
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", arg, benchRunUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "config":
			out.config = value
		case "bench":
			out.bench = strings.TrimSpace(value)
			if out.bench == "" {
				return out, fmt.Errorf("--bench needs a name")
			}
		case "set":
			out.set = strings.TrimSpace(value)
			if out.set == "" {
				return out, fmt.Errorf("--set needs a name")
			}
		case "repeat":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return out, fmt.Errorf("--repeat must be a positive integer")
			}
			out.repeat = n
		case "concurrency":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return out, fmt.Errorf("--concurrency must be a positive integer")
			}
			out.concurrency = n
		case "only":
			out.only = strings.TrimSpace(value)
		case "shadow":
			f, err := strconv.ParseFloat(value, 64)
			if err != nil || f < 0 || f > 1 {
				return out, fmt.Errorf("--shadow must be a number from 0 to 1")
			}
			out.shadow = f
		case "difficulty":
			for _, d := range strings.Split(value, ",") {
				if d = strings.TrimSpace(d); d != "" {
					out.difficulty = append(out.difficulty, d)
				}
			}
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
		case "cheap-thinking":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1024 {
				return out, fmt.Errorf("--cheap-thinking must be a token budget of at least 1024")
			}
			out.cheapThink = n
		case "tag":
			if !validTag(value) {
				return out, fmt.Errorf("--tag must be letters, digits, dash or underscore, starting with a letter or digit")
			}
			out.tag = value
		case "no-cache":
			n, err := strconv.ParseBool(value)
			if err != nil {
				return out, fmt.Errorf("--no-cache must be true or false")
			}
			out.noCache = n
		case "no-evidence":
			n, err := strconv.ParseBool(value)
			if err != nil {
				return out, fmt.Errorf("--no-evidence must be true or false")
			}
			out.noEvidence = n
		case "capture-requests":
			n, err := strconv.ParseBool(value)
			if err != nil {
				return out, fmt.Errorf("--capture-requests must be true or false")
			}
			out.capture = n
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", arg, benchRunUsage)
		}
	}
	if out.config == "" {
		return out, fmt.Errorf("missing --config\n%s", benchRunUsage)
	}
	if err := router.ValidName(out.config); err != nil {
		return out, err
	}
	if out.bench != bench.BenchEcomm && out.bench != bench.BenchBird {
		return out, fmt.Errorf("unknown --bench %q, want ecomm or bird", out.bench)
	}
	if out.noEvidence && out.bench != bench.BenchBird {
		return out, fmt.Errorf("--no-evidence needs --bench bird")
	}
	if (out.routeCtx || out.cheapThink > 0) && out.tag == "" {
		return out, fmt.Errorf("--route-context and --cheap-thinking need --tag so the run lands in its own folder")
	}
	return out, nil
}

func benchRun(args []string) error {
	_, err := runBench(args)
	return err
}

func runBench(args []string) (string, error) {
	opts, err := parseBenchRunArgs(args)
	if err != nil {
		return "", err
	}
	if opts.bench == bench.BenchBird {
		return benchRunBird(opts)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := bench.FindBenchDir(cwd)
	if dir == "" {
		return "", fmt.Errorf("bench/questions.yaml not found")
	}
	qs, err := bench.Load(filepath.Join(dir, bench.QuestionsFile))
	if err != nil {
		return "", err
	}
	entries, err := bench.LoadSnapshot(filepath.Join(dir, bench.AnswersFile))
	if err != nil {
		return "", err
	}
	expected, err := bench.ExpectedByID(entries, qs)
	if err != nil {
		return "", err
	}
	qs, err = selectSet(dir, qs, opts.set)
	if err != nil {
		return "", err
	}
	qs = filterDifficulty(qs, opts.difficulty)
	if len(qs) == 0 {
		return "", fmt.Errorf("no questions match --difficulty %s", strings.Join(opts.difficulty, ","))
	}
	if opts.only != "" {
		qs, err = selectOnly(qs, opts.only)
		if err != nil {
			return "", err
		}
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
	if opts.cheapThink > 0 {
		if err := enableCheapThinking(client, opts.cheapThink); err != nil {
			return "", err
		}
	}
	if cfg.DatabaseURL == "" {
		return "", fmt.Errorf("missing %s", config.DatabaseURLName)
	}
	path, err := semantic.DefaultPath()
	if err != nil {
		return "", err
	}
	sem, err := semantic.Load(path)
	if err != nil {
		return "", err
	}
	jobs := len(qs) * opts.repeat
	waves := (jobs + opts.concurrency - 1) / opts.concurrency
	timeout := time.Duration(waves)*6*time.Minute + 5*time.Minute
	if timeout < 30*time.Minute {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return "", err
	}
	defer pool.Close()
	semText := sem.RenderLevels(opts.levels)
	querier := agent.PoolQuerier{Pool: pool}
	var a bench.AgentRunner
	a = agent.NewPrefix(client, querier, semText)
	if opts.noCache {
		a = agent.NewPrefixNoCache(client, querier, semText)
	}
	if opts.routeCtx {
		a = contextRoutingAgent{inner: a}
	}
	if provider == llm.ProviderMuse {
		ma, err := newMuseAgent(cfg, semText)
		if err != nil {
			return "", err
		}
		a = ma
	}
	r := &bench.Runner{
		Agent:       a,
		Judge:       client,
		Questions:   qs,
		Expected:    expected,
		Config:      opts.config,
		Provider:    provider,
		NoCache:     opts.noCache,
		Repeat:      opts.repeat,
		Concurrency: opts.concurrency,
		Traces:      bench.NewTraceCollector(),
	}
	var routerName string
	if router.IsCascade(opts.config) {
		c, err := newCascade(opts.config, a, client, semText, querier, cfg, opts.shadow, router.PromptOptions{NoCache: opts.noCache}, "")
		if err != nil {
			return "", err
		}
		r.Cascade = c
		routerName = c.Name()
	} else {
		rt, err := newRouter(ctx, opts.config, client, sem, semText, cfg, opts.shadow, router.PromptOptions{NoCache: opts.noCache})
		if err != nil {
			return "", err
		}
		r.Router = rt
		routerName = rt.Name()
	}
	cache := "on"
	if opts.noCache {
		cache = "off"
	}
	fmt.Printf("bench run provider=%s config=%s set=%s router=%s questions=%d repeat=%d concurrency=%d cache=%s levels=%s capture=%t route_context=%t cheap_thinking=%d timeout=%s\n",
		provider, opts.config, opts.set, routerName, len(qs), opts.repeat, opts.concurrency, cache, opts.levels.String(), opts.capture, opts.routeCtx, opts.cheapThink, timeout.Round(time.Minute))
	lines, err := r.Run(ctx)
	if err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102T150405Z")
	outDir := benchRunOutDir(provider, opts)
	outPath := filepath.Join(outDir, stamp+".jsonl")
	if err := bench.WriteLines(outPath, lines); err != nil {
		return "", err
	}
	if err := runHealth(lines, ctx.Err()); err != nil {
		printLines(lines)
		return "", fmt.Errorf("%w; wrote %s but left %s unchanged", err, outPath, filepath.Join(outDir, bench.LatestFileName))
	}
	summary := bench.Summarize(opts.config, stamp, outPath, opts.repeat, lines)
	summary.Provider = provider
	summary.NoCache = opts.noCache
	summary.Tag = opts.tag
	if !opts.levels.IsAll() {
		summary.ContextLevels = opts.levels.String()
	}
	summary = bench.WithOmni(summary, lines)
	latestPath := filepath.Join(outDir, bench.LatestFileName)
	if err := bench.WriteSummary(latestPath, summary); err != nil {
		return "", err
	}
	tracePath := benchTracePath(provider, opts, stamp)
	if err := bench.WriteTraceFile(tracePath, r.Traces.Ordered(len(lines))); err != nil {
		return "", err
	}
	printLines(lines)
	printSummary(summary)
	printShadow(lines)
	fmt.Printf("wrote %s, %s and %s\n", outPath, latestPath, tracePath)
	return outDir, nil
}

func printLines(lines []bench.Line) {
	for _, l := range lines {
		mark := "ok"
		if !l.Correct {
			mark = "WRONG"
		}
		if l.Error != "" {
			mark = "ERROR"
		}
		fmt.Printf("%s rep=%d %s tier=%s label=%s cost=$%.6f route_cost=$%.6f latency=%dms cache_hit=%.2f answer=%q\n",
			l.ID, l.Repeat, mark, l.Tier, l.RouteLabel, l.CostUSD, l.RouteCostUSD, l.LatencyMS, l.CacheHitRatio, truncate(l.Answer, 80))
		if len(l.Attempts) > 0 {
			fmt.Printf("  cascade: %s\n", truncate(l.RouteReason, 200))
		}
		if l.Error != "" {
			fmt.Printf("  error: %s\n", truncate(l.Error, 200))
		}
	}
}

func runHealth(lines []bench.Line, ctxErr error) error {
	errored := 0
	for _, l := range lines {
		if l.Error != "" {
			errored++
		}
	}
	if ctxErr != nil {
		return fmt.Errorf("run stopped early (%v): %d of %d lines errored", ctxErr, errored, len(lines))
	}
	if errored*2 > len(lines) {
		return fmt.Errorf("run unhealthy: %d of %d lines errored", errored, len(lines))
	}
	return nil
}

func selectOnly(qs []bench.Question, spec string) ([]bench.Question, error) {
	byID := map[string]bench.Question{}
	for _, q := range qs {
		byID[q.ID] = q
	}
	var out []bench.Question
	seen := map[string]bool{}
	var missing []string
	for _, id := range strings.Split(spec, ",") {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		q, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		out = append(out, q)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("question %q not found", strings.Join(missing, ", "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--only needs at least one question id")
	}
	return out, nil
}

func validTag(tag string) bool {
	if tag == "" {
		return false
	}
	for i := 0; i < len(tag); i++ {
		c := tag[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
		if !ok || i == 0 && (c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func benchRunOutDir(provider string, opts benchRunArgs) string {
	return bench.ConfigDir(provider, benchRunName(opts))
}

func benchRunName(opts benchRunArgs) string {
	name := opts.config
	if opts.noCache {
		name += "-nocache"
	}
	if opts.set != bench.SetFull {
		name += "-" + opts.set
	}
	if !opts.levels.IsAll() {
		name += "-levels-" + opts.levels.Slug()
	}
	return name + filterSuffix(opts)
}

func benchTracePath(provider string, opts benchRunArgs, stamp string) string {
	return bench.TraceFilePath(provider, benchRunName(opts), false, stamp)
}

func filterSuffix(opts benchRunArgs) string {
	out := ""
	if opts.only != "" || len(opts.difficulty) > 0 {
		out += "-subset"
	}
	if opts.shadow > 0 {
		out += "-shadow"
	}
	if opts.tag != "" {
		out += "-" + opts.tag
	}
	return out
}

func selectSet(benchDir string, qs []bench.Question, name string) ([]bench.Question, error) {
	if name == bench.SetFull {
		return qs, nil
	}
	set, err := bench.LoadSet(bench.SetPath(benchDir, name))
	if err != nil {
		return nil, err
	}
	return bench.SelectSet(qs, set)
}

func filterDifficulty(qs []bench.Question, keep []string) []bench.Question {
	if len(keep) == 0 {
		return qs
	}
	var out []bench.Question
	for _, q := range qs {
		if slices.Contains(keep, q.Difficulty) {
			out = append(out, q)
		}
	}
	return out
}

func printSummary(s bench.Summary) {
	fmt.Printf("config=%s total=%d correct=%d accuracy=%.3f strict=%d accuracy_strict=%.3f\n", s.Config, s.Total, s.Correct, s.Accuracy, s.CorrectStrict, s.AccuracyStrict)
	if s.EXTotal > 0 {
		fmt.Println(exLine(s))
	}
	difficulties := []string{bench.DifficultyEasy, bench.DifficultyModerate, bench.DifficultyHard, bench.DifficultyExpert}
	for _, d := range []string{bench.DifficultySimple, bench.DifficultyChallenging} {
		if _, ok := s.ByDifficulty[d]; ok {
			difficulties = append(difficulties, d)
		}
	}
	for _, d := range difficulties {
		b := s.ByDifficulty[d]
		line := fmt.Sprintf("  %s: %d/%d accuracy=%.3f strict=%d accuracy_strict=%.3f", d, b.Correct, b.Total, b.Accuracy, b.CorrectStrict, b.AccuracyStrict)
		if s.EXTotal > 0 {
			line += fmt.Sprintf(" ex=%d accuracy_ex=%.3f", b.EXCorrect, b.EXAccuracy)
		}
		fmt.Println(line)
	}
	fmt.Printf("total_cost=$%.6f route_cost=$%.6f judge_cost=$%.6f cost_per_correct=$%.6f\n",
		s.TotalCostUSD, s.RouteCostUSD, s.JudgeCostUSD, s.CostPerCorrectUSD)
	fmt.Printf("route_match=%d/%d\n%s", s.RouteMatch, s.Total, bench.ConfusionTable(s.Confusion))
	fmt.Print(bench.CascadeReport(s))
	median := int64(0)
	if s.Omni != nil {
		median = s.Omni.MedianLatencyMS
	}
	fmt.Printf("latency median=%dms mean=%.0fms p95=%dms\n", median, s.MeanLatencyMS, s.P95LatencyMS)
	if s.Omni != nil {
		fmt.Println(omniLine(s))
	}
}

func shadowCost(lines []bench.Line) (float64, int) {
	var total float64
	n := 0
	add := func(g *router.GateInfo) {
		if g != nil && g.Shadowed {
			total += g.ShadowCost
			n++
		}
	}
	for _, l := range lines {
		add(l.RouteGate)
		for _, a := range l.Attempts {
			add(a.VerifyGate)
		}
	}
	return total, n
}

func printShadow(lines []bench.Line) {
	if cost, n := shadowCost(lines); n > 0 {
		fmt.Printf("shadow_cost=$%.6f shadow_calls=%d (not included in total_cost)\n", cost, n)
	}
}

func exLine(s bench.Summary) string {
	return fmt.Sprintf("ex: correct=%d ex_scored=%d total=%d accuracy=%.3f (correct/total) scored_accuracy=%.3f (correct/ex_scored) cost_per_ex_correct=$%.6f",
		s.EXCorrect, s.EXTotal, s.Total, s.EXAccuracy, ratio(s.EXCorrect, s.EXTotal), s.CostPerEXCorrectUSD)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
