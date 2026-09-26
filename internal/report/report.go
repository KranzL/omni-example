package report

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	SummaryFile    = "summary.md"
	ParetoFile     = "pareto.svg"
	DifficultyFile = "difficulty.svg"
	ReadmeStart    = "<!-- report:start -->"
	ReadmeEnd      = "<!-- report:end -->"
)

var Difficulties = []string{bench.DifficultyEasy, bench.DifficultyModerate, bench.DifficultyHard, bench.DifficultyExpert}

var Tiers = []llm.Tier{llm.TierCheap, llm.TierMid, llm.TierTop}

type Config struct {
	Label         string
	Path          string
	Provider      string
	Baseline      bool
	Summary       bench.Summary
	Lines         []bench.Line
	CostUSD       float64
	CacheHitRatio float64
	TierMix       map[llm.Tier]float64
	Wrong         []bench.Line
}

func (c Config) Questions() int {
	seen := map[string]bool{}
	for _, l := range c.Lines {
		seen[l.ID] = true
	}
	return len(seen)
}

func (c Config) Passes() int {
	if c.Summary.Repeat < 1 {
		return 1
	}
	return c.Summary.Repeat
}

func (c Config) PassCostUSD() float64 {
	if c.Summary.Repeat <= 1 {
		return c.CostUSD
	}
	return c.CostUSD / float64(c.Summary.Repeat)
}

func (c Config) CostPerCorrect() float64 {
	if c.Summary.Correct == 0 {
		return 0
	}
	return c.CostUSD / float64(c.Summary.Correct)
}

func IsBaseline(config string) bool {
	return strings.HasPrefix(config, "always-")
}

func FindLatest(resultsDir string) ([]string, error) {
	top, err := filepath.Glob(filepath.Join(resultsDir, "*", bench.LatestFileName))
	if err != nil {
		return nil, err
	}
	nested, err := filepath.Glob(filepath.Join(resultsDir, "*", "*", bench.LatestFileName))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range append(top, nested...) {
		rel, err := filepath.Rel(resultsDir, p)
		if err == nil && strings.HasPrefix(filepath.ToSlash(rel), "bird/") {
			continue
		}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func Load(root string, paths []string) ([]Config, error) {
	var out []Config
	for _, p := range paths {
		s, err := bench.ReadSummary(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		lines, err := bench.ReadLines(filepath.Join(root, s.Source))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		out = append(out, Build(filepath.ToSlash(rel), s, lines))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		if out[i].Baseline != out[j].Baseline {
			return out[i].Baseline
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}

func Build(path string, s bench.Summary, lines []bench.Line) Config {
	provider := s.Provider
	if provider == "" {
		provider = llm.ProviderAnthropic
	}
	c := Config{
		Label:    provider + "/" + labelFromPath(path, s.Config),
		Path:     path,
		Provider: provider,
		Baseline: IsBaseline(s.Config),
		Summary:  s,
		Lines:    lines,
		CostUSD:  s.TotalCostUSD + s.JudgeCostUSD,
		TierMix:  map[llm.Tier]float64{},
	}
	var read, prompt int64
	tiers := map[llm.Tier]int{}
	for _, l := range lines {
		read += l.Tokens.CacheReadInputTokens
		prompt += l.Tokens.PromptTokens()
		if l.Tier != "" {
			tiers[l.Tier]++
		}
		if !l.Correct {
			c.Wrong = append(c.Wrong, l)
		}
	}
	if prompt > 0 {
		c.CacheHitRatio = float64(read) / float64(prompt)
	}
	if len(lines) > 0 {
		for t, n := range tiers {
			c.TierMix[t] = float64(n) / float64(len(lines))
		}
	}
	return c
}

func labelFromPath(path, config string) string {
	dir := filepath.Base(filepath.Dir(path))
	if dir == "." || dir == "" {
		return config
	}
	return dir
}

func FullSetSize(cs []Config) int {
	n := 0
	for _, c := range cs {
		if q := c.Questions(); q > n {
			n = q
		}
	}
	return n
}

func Comparable(cs []Config) []Config {
	full := FullSetSize(cs)
	var out []Config
	for _, c := range cs {
		if c.Questions() == full {
			out = append(out, c)
		}
	}
	return out
}

func Cheapest(cs []Config) (Config, bool) {
	var best Config
	found := false
	for _, c := range cs {
		if c.Summary.Correct == 0 {
			continue
		}
		if !found || c.CostPerCorrect() < best.CostPerCorrect() {
			best, found = c, true
		}
	}
	return best, found
}

func Frontier(cs []Config) map[string]bool {
	out := map[string]bool{}
	for _, c := range cs {
		dominated := false
		for _, o := range cs {
			if o.Label == c.Label || o.Provider != c.Provider {
				continue
			}
			if o.PassCostUSD() <= c.PassCostUSD() && o.Summary.Accuracy >= c.Summary.Accuracy && (o.PassCostUSD() < c.PassCostUSD() || o.Summary.Accuracy > c.Summary.Accuracy) {
				dominated = true
				break
			}
		}
		if !dominated {
			out[c.Label] = true
		}
	}
	return out
}

type Files struct {
	Summary    string
	Pareto     string
	Difficulty string
}

func Generate(cs []Config) Files {
	return Files{
		Summary:    Markdown(cs),
		Pareto:     ParetoSVG(Comparable(cs)),
		Difficulty: DifficultySVG(cs),
	}
}

func Write(resultsDir string, f Files) error {
	for name, body := range map[string]string{SummaryFile: f.Summary, ParetoFile: f.Pareto, DifficultyFile: f.Difficulty} {
		if err := os.WriteFile(filepath.Join(resultsDir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func EmbedReadme(readme, section string) (string, error) {
	block := ReadmeStart + "\n" + section + ReadmeEnd
	starts, ends := strings.Count(readme, ReadmeStart), strings.Count(readme, ReadmeEnd)
	if starts == 0 && ends == 0 {
		return strings.TrimRight(readme, "\n") + "\n\n## Report\n\n" + block + "\n", nil
	}
	if starts != 1 || ends != 1 {
		return "", fmt.Errorf("README has %d %s and %d %s markers, want one of each", starts, ReadmeStart, ends, ReadmeEnd)
	}
	start := strings.Index(readme, ReadmeStart)
	end := strings.Index(readme, ReadmeEnd)
	if end < start {
		return "", fmt.Errorf("README has %s before %s", ReadmeEnd, ReadmeStart)
	}
	return readme[:start] + block + readme[end+len(ReadmeEnd):], nil
}
