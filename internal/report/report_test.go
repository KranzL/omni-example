package report

import (
	"encoding/xml"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/llm"
)

func cfg(provider, config string, correct int, cost float64, lines []bench.Line) Config {
	s := bench.Summary{Config: config, Provider: provider, Total: 4, Correct: correct, TotalCostUSD: cost, Accuracy: float64(correct) / 4,
		ByDifficulty: map[string]bench.DifficultySummary{bench.DifficultyEasy: {Total: 4, Correct: correct, Accuracy: float64(correct) / 4}}}
	return Build("results/"+provider+"/"+config+"/latest.json", s, lines)
}

func fourLines(tier llm.Tier, wrong int) []bench.Line {
	var out []bench.Line
	for i := 0; i < 4; i++ {
		l := bench.Line{ID: []string{"e01", "e02", "e03", "e04"}[i], Difficulty: bench.DifficultyEasy, Tier: tier, Correct: i >= wrong,
			Tokens: llm.Usage{InputTokens: 10, CacheReadInputTokens: 30}}
		if !l.Correct {
			l.FailReason = "wrong_number: got 1, want 2"
			l.Answer = "a | b"
		}
		out = append(out, l)
	}
	return out
}

func TestBuildAndPick(t *testing.T) {
	router := fourLines(llm.TierCheap, 1)
	router[1].Tier = llm.TierTop
	cs := []Config{
		cfg("anthropic", "always-mid", 4, 0.40, fourLines(llm.TierMid, 0)),
		cfg("anthropic", "always-cheap", 3, 0.10, fourLines(llm.TierCheap, 1)),
		cfg("anthropic", "heuristic", 3, 0.20, router),
		cfg("venice", "always-mid", 4, 0.02, fourLines(llm.TierMid, 0)),
	}
	if cs[0].Label != "anthropic/always-mid" || !cs[0].Baseline || cs[2].Baseline {
		t.Fatalf("labels %+v", cs[0])
	}
	if cs[0].CacheHitRatio != 0.75 {
		t.Errorf("cache hit %v", cs[0].CacheHitRatio)
	}
	if got := TierMix(cs[2]); got != "75/0/25" {
		t.Errorf("tier mix %q", got)
	}
	if len(cs[1].Wrong) != 1 {
		t.Errorf("wrong %d", len(cs[1].Wrong))
	}
	best, ok := Cheapest(cs)
	if !ok || best.Label != "venice/always-mid" {
		t.Errorf("cheapest %s", best.Label)
	}
	front := Frontier(cs)
	for label, want := range map[string]bool{"anthropic/always-mid": true, "anthropic/always-cheap": true, "anthropic/heuristic": false, "venice/always-mid": true} {
		if front[label] != want {
			t.Errorf("frontier %s = %v", label, front[label])
		}
	}
	f := Generate(cs)
	for _, want := range []string{"| anthropic/heuristic | router |", "75/0/25", "Lowest cost per correct answer on the full 4-question set: venice/always-mid", "a \\| b", "wrong_number: got 1, want 2"} {
		if !strings.Contains(f.Summary, want) {
			t.Errorf("summary lacks %q", want)
		}
	}
	for name, svg := range map[string]string{"pareto": f.Pareto, "difficulty": f.Difficulty} {
		wellFormed(t, name, svg)
	}
	if !strings.Contains(f.Summary, "| anthropic/heuristic | router | 1 | 0.750 (3/4) |") {
		t.Error("summary row lacks the passes column")
	}
	if !strings.Contains(f.Summary, "(4/4 correct over 1 pass, total $0.0200)") {
		t.Error("headline lacks the pass count")
	}
	if !strings.Contains(f.Pareto, "<rect class=\"p2\"") || !strings.Contains(f.Pareto, "<circle class=\"p1\"") {
		t.Error("pareto lacks baseline or router marks")
	}
}

func wellFormed(t *testing.T, name, s string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s is not well-formed XML: %v", name, err)
		}
	}
}

func TestEmbedReadme(t *testing.T) {
	once, err := EmbedReadme("# x\n\ntext\n", "table\n")
	if err != nil || !strings.Contains(once, "## Report\n\n"+ReadmeStart+"\ntable\n"+ReadmeEnd) {
		t.Fatalf("first embed: %v\n%s", err, once)
	}
	twice, err := EmbedReadme(once, "new\n")
	if err != nil || strings.Count(twice, ReadmeStart) != 1 || !strings.Contains(twice, "new\n"+ReadmeEnd) || strings.Contains(twice, "table") {
		t.Fatalf("second embed: %v\n%s", err, twice)
	}
}

func TestEmbedReadmeRejectsMalformedMarkers(t *testing.T) {
	for name, readme := range map[string]string{
		"start only":   "# x\n" + ReadmeStart + "\nold\n",
		"end only":     "# x\nold\n" + ReadmeEnd + "\n",
		"end first":    "# x\n" + ReadmeEnd + "\nkeep\n" + ReadmeStart + "\n",
		"two starts":   ReadmeStart + "\n" + ReadmeStart + "\n" + ReadmeEnd + "\n",
		"two sections": ReadmeStart + "a" + ReadmeEnd + ReadmeStart + "b" + ReadmeEnd,
	} {
		if out, err := EmbedReadme(readme, "new\n"); err == nil {
			t.Errorf("%s: want error, got\n%s", name, out)
		}
	}
}

func TestCommittedResults(t *testing.T) {
	root := filepath.Join("..", "..")
	paths, err := FindLatest(filepath.Join(root, bench.ResultsDirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Skip("no committed results")
	}
	cs, err := Load(root, paths)
	if err != nil {
		t.Fatal(err)
	}
	f := Generate(cs)
	wellFormed(t, "pareto", f.Pareto)
	wellFormed(t, "difficulty", f.Difficulty)
	for _, c := range cs {
		if !strings.Contains(f.Summary, "| "+c.Label+" |") {
			t.Errorf("summary has no row for %s", c.Label)
		}
	}
}

func TestFindLatestSkipsBird(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"always-mid", "venice/always-mid", "bird/always-mid", "bird/venice/always-mid"} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "latest.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := FindLatest(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "always-mid", "latest.json"), filepath.Join(dir, "venice", "always-mid", "latest.json")}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("FindLatest = %v, want %v", got, want)
	}
}

func TestFrontierUsesCostPerPass(t *testing.T) {
	a := Config{Label: "a", Provider: "p", CostUSD: 0.30, Summary: bench.Summary{Repeat: 3, Accuracy: 0.95}}
	b := Config{Label: "b", Provider: "p", CostUSD: 0.20, Summary: bench.Summary{Repeat: 1, Accuracy: 0.90}}
	if math.Abs(a.PassCostUSD()-0.1) > 1e-12 {
		t.Fatalf("pass cost %v", a.PassCostUSD())
	}
	front := Frontier([]Config{a, b})
	if !front["a"] || front["b"] {
		t.Fatalf("frontier %v, want only a", front)
	}
}

func TestParetoSVGZeroCost(t *testing.T) {
	cs := []Config{
		{Label: "free", Provider: "p", CostUSD: 0, Summary: bench.Summary{Repeat: 1, Accuracy: 0.5, Total: 32}},
		{Label: "paid", Provider: "p", CostUSD: 0.2, Summary: bench.Summary{Repeat: 1, Accuracy: 0.9, Total: 32}},
	}
	if svg := ParetoSVG(cs); strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
		t.Fatal("svg contains NaN or Inf")
	}
}

func TestDifficultyCellsShowPasses(t *testing.T) {
	lines := func(repeats int) []bench.Line {
		var out []bench.Line
		for r := 1; r <= repeats; r++ {
			for _, id := range []string{"e01", "e02", "e03", "e04", "e05", "e06", "e07", "e08"} {
				out = append(out, bench.Line{ID: id, Difficulty: bench.DifficultyEasy, Repeat: r, Correct: true})
			}
		}
		return out
	}
	three := Build("results/anthropic/tri/latest.json", bench.Summarize("tri", "ts", "src", 3, lines(3)), lines(3))
	one := Build("results/anthropic/uno/latest.json", bench.Summarize("uno", "ts", "src", 1, lines(1)), lines(1))
	table := Table([]Config{three, one})
	for _, want := range []string{"| anthropic/tri | router | 3 | 1.000 (24/24) |", " 24/24 (3 passes) |", "| anthropic/uno | router | 1 | 1.000 (8/8) |", " 8/8 |"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	svg := DifficultySVG([]Config{three, one})
	for _, want := range []string{"24/24, 3 passes,", "8/8, 1 pass,", "counted once per pass"} {
		if !strings.Contains(svg, want) {
			t.Errorf("svg lacks %q", want)
		}
	}
	best := Headline([]Config{three})
	if !strings.Contains(best, "(24/24 correct over 3 passes,") {
		t.Errorf("headline %q", best)
	}
}

func TestStaticTextMatchesData(t *testing.T) {
	full := cfg("anthropic", "always-mid", 4, 0.40, fourLines(llm.TierMid, 0))
	full.Summary.Regraded = &bench.RegradeInfo{At: "t"}
	small := cfg("anthropic", "always-mid-verify", 1, 0.10, fourLines(llm.TierMid, 0)[:3])
	md := Markdown([]Config{full, small})
	if strings.Contains(md, "predate the expert tier") || strings.Contains(md, "for every committed run file") {
		t.Errorf("stale static text:\n%s", md)
	}
	for _, want := range []string{"Runs on fewer than 4 questions are left out of the Pareto chart and the lowest-cost pick: anthropic/always-mid-verify (3).", "1 of the 2 configurations above were regraded", "Not regraded: anthropic/always-mid-verify."} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
}
