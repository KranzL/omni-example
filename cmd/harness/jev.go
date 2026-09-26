package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/jev"
	"github.com/KranzL/omni-example/internal/router"
	"github.com/KranzL/omni-example/internal/semantic"
)

const jevCompareUsage = `usage: harness jev compare --set seeds|bench [--difficulty easy,moderate,hard] [--provider anthropic|venice] [--concurrency K]`

const (
	jevSetSeeds  = "seeds"
	jevSetBench  = "bench"
	jevShadowDir = "results/jev-shadow"
)

var jevCompareThresholds = []float64{0, 0.5, 0.6, 0.7, 0.8, 0.85, 0.9, 0.95, 0.99}

type compareItem struct {
	ID         string
	Difficulty string
	Text       string
}

type CompareLine struct {
	ID           string             `json:"id"`
	Set          string             `json:"set"`
	Difficulty   string             `json:"difficulty"`
	Provider     string             `json:"provider"`
	JevLabel     string             `json:"jev_label"`
	JevScore     float64            `json:"jev_confidence"`
	JevProbs     map[string]float64 `json:"jev_probs"`
	JevReason    string             `json:"jev_reason"`
	JevCostUSD   float64            `json:"jev_cost_usd"`
	JevTokens    int64              `json:"jev_input_tokens"`
	JevLatencyMS int64              `json:"jev_latency_ms"`
	JevModel     string             `json:"jev_model"`
	JevError     string             `json:"jev_error,omitempty"`
	LLMLabel     string             `json:"llm_label"`
	LLMReason    string             `json:"llm_reason"`
	LLMCostUSD   float64            `json:"llm_cost_usd"`
	LLMLatencyMS int64              `json:"llm_latency_ms"`
	LLMModel     string             `json:"llm_model"`
	LLMError     string             `json:"llm_error,omitempty"`
}

type jevCompareArgs struct {
	set         string
	difficulty  []string
	provider    string
	concurrency int
}

func parseJevCompareArgs(args []string) (jevCompareArgs, error) {
	out := jevCompareArgs{concurrency: 4}
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		if !strings.HasPrefix(args[i], "-") {
			return out, fmt.Errorf("unexpected argument %s\n%s", args[i], jevCompareUsage)
		}
		if !hasValue {
			if i+1 >= len(args) {
				return out, fmt.Errorf("flag %s needs a value\n%s", args[i], jevCompareUsage)
			}
			i++
			value = args[i]
		}
		switch name {
		case "set":
			out.set = value
		case "difficulty":
			for _, d := range strings.Split(value, ",") {
				if d = strings.TrimSpace(d); d != "" {
					out.difficulty = append(out.difficulty, d)
				}
			}
		case "provider":
			out.provider = value
		case "concurrency":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return out, fmt.Errorf("--concurrency must be a positive integer")
			}
			out.concurrency = n
		default:
			return out, fmt.Errorf("unknown flag %s\n%s", args[i], jevCompareUsage)
		}
	}
	if out.set != jevSetSeeds && out.set != jevSetBench {
		return out, fmt.Errorf("--set must be seeds or bench\n%s", jevCompareUsage)
	}
	return out, nil
}

func jevCmd(args []string) error {
	if len(args) == 0 || args[0] != "compare" {
		return fmt.Errorf("%s", jevCompareUsage)
	}
	opts, err := parseJevCompareArgs(args[1:])
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := bench.FindBenchDir(cwd)
	if dir == "" {
		return fmt.Errorf("bench directory not found")
	}
	items, err := loadCompareItems(dir, opts.set)
	if err != nil {
		return err
	}
	if len(opts.difficulty) > 0 {
		var kept []compareItem
		for _, it := range items {
			if slices.Contains(opts.difficulty, it.Difficulty) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.JevAPIKey == "" {
		return fmt.Errorf("missing %s", config.JevAPIKeyName)
	}
	provider, err := resolveProvider(cfg, opts.provider)
	if err != nil {
		return err
	}
	client, err := newProvider(cfg, provider)
	if err != nil {
		return err
	}
	path, err := semantic.DefaultPath()
	if err != nil {
		return err
	}
	sem, err := semantic.Load(path)
	if err != nil {
		return err
	}
	semText := sem.Render()
	jc := router.NewJevClassifier(jev.NewClient(cfg.JevAPIKey), semText)
	lc := router.NewClassifier(client, semText)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	fmt.Printf("jev compare set=%s items=%d provider=%s concurrency=%d\n", opts.set, len(items), provider, opts.concurrency)
	lines := make([]CompareLine, len(items))
	sem4 := make(chan struct{}, opts.concurrency)
	var wg sync.WaitGroup
	for i, it := range items {
		wg.Add(1)
		go func(i int, it compareItem) {
			defer wg.Done()
			sem4 <- struct{}{}
			defer func() { <-sem4 }()
			lines[i] = compareOne(ctx, jc, lc, it, opts.set, provider)
		}(i, it)
	}
	wg.Wait()
	stamp := time.Now().UTC().Format("20060102T150405Z")
	out := filepath.Join(jevShadowDir, fmt.Sprintf("%s-%s-%s.jsonl", opts.set, provider, stamp))
	if err := writeCompareLines(out, lines); err != nil {
		return err
	}
	for _, l := range lines {
		fmt.Printf("%s %s jev=%s conf=%.2f llm=%s jev_ms=%d llm_ms=%d%s%s\n", l.ID, l.Difficulty, l.JevLabel, l.JevScore, l.LLMLabel, l.JevLatencyMS, l.LLMLatencyMS, errSuffix("jev", l.JevError), errSuffix("llm", l.LLMError))
	}
	fmt.Print(CompareReport(lines))
	fmt.Printf("wrote %s\n", out)
	return jevCompareHealth(lines)
}

func jevCompareHealth(lines []CompareLine) error {
	for _, l := range lines {
		if l.JevError == "" && l.LLMError == "" {
			return nil
		}
	}
	return fmt.Errorf("jev compare: no comparable lines out of %d", len(lines))
}

func errSuffix(who, e string) string {
	if e == "" {
		return ""
	}
	return fmt.Sprintf(" %s_error=%q", who, truncate(e, 120))
}

func compareOne(ctx context.Context, jc *router.JevClassifier, lc *router.Classifier, it compareItem, set, provider string) CompareLine {
	l := CompareLine{ID: it.ID, Set: set, Difficulty: it.Difficulty, Provider: provider}
	in := router.DecisionInput{Question: it.Text}
	j, err := jc.Decide(ctx, in)
	l.JevLabel, l.JevScore, l.JevProbs, l.JevReason = j.Label, j.Score, j.Probs, j.Reason
	l.JevCostUSD, l.JevLatencyMS = j.Cost, j.Latency.Milliseconds()
	for _, r := range j.CallRecords {
		l.JevTokens += r.Usage.InputTokens
		l.JevModel = r.Model
	}
	if err != nil {
		l.JevError = err.Error()
	}
	c, err := lc.Decide(ctx, in)
	if err != nil && ctx.Err() == nil {
		retry, rerr := lc.Decide(ctx, in)
		retry.Cost += c.Cost
		retry.CallRecords = append(c.CallRecords, retry.CallRecords...)
		c, err = retry, rerr
	}
	l.LLMLabel, l.LLMReason, l.LLMCostUSD, l.LLMLatencyMS = c.Label, c.Reason, c.Cost, c.Latency.Milliseconds()
	if n := len(c.CallRecords); n > 0 {
		l.LLMModel = c.CallRecords[n-1].Model
	}
	if err != nil {
		l.LLMError = err.Error()
	}
	return l
}

func loadCompareItems(dir, set string) ([]compareItem, error) {
	var out []compareItem
	if set == jevSetSeeds {
		seeds, err := router.LoadSeeds(filepath.Join(dir, bench.RouterSeedFile))
		if err != nil {
			return nil, err
		}
		for _, s := range seeds {
			out = append(out, compareItem{ID: s.ID, Difficulty: s.Label, Text: s.Question})
		}
		return out, nil
	}
	qs, err := bench.Load(filepath.Join(dir, bench.QuestionsFile))
	if err != nil {
		return nil, err
	}
	for _, q := range qs {
		out = append(out, compareItem{ID: q.ID, Difficulty: q.Difficulty, Text: q.Text})
	}
	return out, nil
}

func writeCompareLines(path string, lines []CompareLine) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, l := range lines {
		if err := enc.Encode(l); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func CompareReport(lines []CompareLine) string {
	var b strings.Builder
	var n, agree, jevHand, llmHand int
	var jevCost, llmCost float64
	var jevMS, llmMS []int64
	for _, l := range lines {
		if l.JevError != "" || l.LLMError != "" {
			continue
		}
		n++
		if l.JevLabel == l.LLMLabel {
			agree++
		}
		if l.JevLabel == handLabel(l.Difficulty) {
			jevHand++
		}
		if l.LLMLabel == handLabel(l.Difficulty) {
			llmHand++
		}
		jevCost += l.JevCostUSD
		llmCost += l.LLMCostUSD
		jevMS = append(jevMS, l.JevLatencyMS)
		llmMS = append(llmMS, l.LLMLatencyMS)
	}
	if n == 0 {
		return "no comparable lines\n"
	}
	fmt.Fprintf(&b, "comparable=%d of %d\n", n, len(lines))
	fmt.Fprintf(&b, "jev_vs_llm agree=%d/%d disagreement=%.3f\n", agree, n, 1-float64(agree)/float64(n))
	fmt.Fprintf(&b, "jev_vs_hand=%d/%d llm_vs_hand=%d/%d\n", jevHand, n, llmHand, n)
	fmt.Fprintf(&b, "cost jev=$%.6f llm=$%.6f\n", jevCost, llmCost)
	fmt.Fprintf(&b, "latency_ms jev mean=%.0f p50=%d p95=%d; llm mean=%.0f p50=%d p95=%d\n", meanMS(jevMS), pct(jevMS, 50), pct(jevMS, 95), meanMS(llmMS), pct(llmMS, 50), pct(llmMS, 95))
	fmt.Fprintf(&b, "confusion jev (rows hand label, columns jev label):\n%s", labelConfusion(lines, func(l CompareLine) string { return l.JevLabel }))
	fmt.Fprintf(&b, "confusion llm (rows hand label, columns llm label):\n%s", labelConfusion(lines, func(l CompareLine) string { return l.LLMLabel }))
	fmt.Fprintf(&b, "threshold sweep (one threshold for every label):\n")
	fmt.Fprintf(&b, "%-9s %-8s %-18s %-14s %-14s\n", "threshold", "kept", "kept_disagree_llm", "gated_vs_llm", "gated_vs_hand")
	for _, t := range jevCompareThresholds {
		var kept, keptDis, gLLM, gHand int
		for _, l := range lines {
			if l.JevError != "" || l.LLMError != "" {
				continue
			}
			label := l.LLMLabel
			if l.JevScore >= t {
				kept++
				label = l.JevLabel
				if l.JevLabel != l.LLMLabel {
					keptDis++
				}
			}
			if label == l.LLMLabel {
				gLLM++
			}
			if label == handLabel(l.Difficulty) {
				gHand++
			}
		}
		fmt.Fprintf(&b, "%-9.2f %-8s %-18s %-14s %-14s\n", t, fmt.Sprintf("%d/%d", kept, n), fmt.Sprintf("%d/%d", keptDis, kept), fmt.Sprintf("%d/%d", gLLM, n), fmt.Sprintf("%d/%d", gHand, n))
	}
	return b.String()
}

func labelConfusion(lines []CompareLine, pick func(CompareLine) string) string {
	labels := router.DifficultyLabels()
	var b strings.Builder
	fmt.Fprintf(&b, "  %-9s", "")
	for _, c := range labels {
		fmt.Fprintf(&b, " %-9s", c)
	}
	b.WriteString("\n")
	for _, r := range labels {
		fmt.Fprintf(&b, "  %-9s", r)
		for _, c := range labels {
			n := 0
			for _, l := range lines {
				if l.JevError != "" || l.LLMError != "" {
					continue
				}
				if handLabel(l.Difficulty) == r && pick(l) == c {
					n++
				}
			}
			fmt.Fprintf(&b, " %-9d", n)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func handLabel(difficulty string) string {
	if difficulty == bench.DifficultyExpert {
		return router.LabelHard
	}
	return difficulty
}

func meanMS(v []int64) float64 {
	if len(v) == 0 {
		return 0
	}
	var t int64
	for _, x := range v {
		t += x
	}
	return float64(t) / float64(len(v))
}

func pct(v []int64, p int) int64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int64{}, v...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := (p*len(s)+99)/100 - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(s) {
		idx = len(s) - 1
	}
	return s[idx]
}
