package bench

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"unicode/utf8"
)

type Side struct {
	Label   string
	Path    string
	Lines   []Line
	Summary Summary
}

type QuestionOutcome struct {
	Correct    int
	Total      int
	Answer     string
	CostUSD    float64
	FailAnswer string
	FailReason string
}

func (o QuestionOutcome) Verdict() string {
	if o.Total == 0 {
		return "missing"
	}
	if o.Total == 1 {
		if o.Correct == 1 {
			return "pass"
		}
		return "fail"
	}
	return fmt.Sprintf("%d/%d", o.Correct, o.Total)
}

type Flip struct {
	ID         string
	Difficulty string
	A          QuestionOutcome
	B          QuestionOutcome
}

type Comparison struct {
	A     Side
	B     Side
	Flips []Flip
}

func NewSide(label, path string, lines []Line) Side {
	config := ""
	if len(lines) > 0 {
		config = lines[0].Config
	}
	repeat := 0
	for _, l := range lines {
		repeat = max(repeat, l.Repeat)
	}
	stamp := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s := WithOmni(Summarize(config, stamp, path, repeat, lines), lines)
	if len(lines) > 0 {
		s.Provider = lines[0].Provider
	}
	return Side{Label: label, Path: path, Lines: lines, Summary: s}
}

func outcomes(lines []Line) (map[string]QuestionOutcome, map[string]string, []string) {
	out := map[string]QuestionOutcome{}
	diff := map[string]string{}
	var order []string
	for _, l := range lines {
		o, ok := out[l.ID]
		if !ok {
			order = append(order, l.ID)
			o.Answer = l.Answer
			diff[l.ID] = l.Difficulty
		}
		o.Total++
		if l.Correct {
			o.Correct++
		} else if o.Total-o.Correct == 1 {
			o.FailAnswer = l.Answer
			o.FailReason = l.FailReason
		}
		o.CostUSD += l.CostUSD
		out[l.ID] = o
	}
	for id, o := range out {
		o.CostUSD /= float64(o.Total)
		out[id] = o
	}
	return out, diff, order
}

func Compare(a, b Side) Comparison {
	ao, adiff, aorder := outcomes(a.Lines)
	bo, bdiff, border := outcomes(b.Lines)
	order := append([]string(nil), aorder...)
	for _, id := range border {
		if _, ok := ao[id]; !ok {
			order = append(order, id)
		}
	}
	c := Comparison{A: a, B: b}
	for _, id := range order {
		x, y := ao[id], bo[id]
		if x.Total > 0 && y.Total > 0 && x.Correct*y.Total == y.Correct*x.Total {
			continue
		}
		d := adiff[id]
		if d == "" {
			d = bdiff[id]
		}
		c.Flips = append(c.Flips, Flip{ID: id, Difficulty: d, A: x, B: y})
	}
	return c
}

func (c Comparison) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "A: %s (%s)\nB: %s (%s)\n\n", c.A.Label, c.A.Path, c.B.Label, c.B.Path)
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "metric\tA\tB")
	for _, row := range c.rows() {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row[0], row[1], row[2])
	}
	tw.Flush()
	fmt.Fprintf(&sb, "\nverdict flips: %d\n", len(c.Flips))
	if len(c.Flips) == 0 {
		return sb.String()
	}
	tw = tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tdifficulty\tA verdict\tA answer\tA cost\tB verdict\tB answer\tB cost")
	for _, f := range c.Flips {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t$%.6f\t%s\t%s\t$%.6f\n", f.ID, f.Difficulty,
			f.A.Verdict(), oneLine(f.A.Answer, 40), f.A.CostUSD,
			f.B.Verdict(), oneLine(f.B.Answer, 40), f.B.CostUSD)
	}
	tw.Flush()
	return sb.String()
}

func (c Comparison) rows() [][3]string {
	a, b := c.A.Summary, c.B.Summary
	am, bm := omniOf(a), omniOf(b)
	rows := [][3]string{
		{"questions x repeats", fmt.Sprintf("%d x %d", am.Questions, am.Repeats), fmt.Sprintf("%d x %d", bm.Questions, bm.Repeats)},
		{"accuracy", ratio(a.Correct, a.Total), ratio(b.Correct, b.Total)},
		{"accuracy_strict", ratio(a.CorrectStrict, a.Total), ratio(b.CorrectStrict, b.Total)},
	}
	for _, d := range []string{DifficultyEasy, DifficultyModerate, DifficultyHard, DifficultyExpert} {
		x, y := a.ByDifficulty[d], b.ByDifficulty[d]
		if x.Total == 0 && y.Total == 0 {
			continue
		}
		rows = append(rows, [3]string{"  " + d, fmt.Sprintf("%d/%d", x.Correct, x.Total), fmt.Sprintf("%d/%d", y.Correct, y.Total)})
	}
	rows = append(rows,
		[3]string{"total cost", fmt.Sprintf("$%.6f", a.TotalCostUSD+a.JudgeCostUSD), fmt.Sprintf("$%.6f", b.TotalCostUSD+b.JudgeCostUSD)},
		[3]string{"cost per correct", fmt.Sprintf("$%.6f", a.CostPerCorrectUSD), fmt.Sprintf("$%.6f", b.CostPerCorrectUSD)},
		[3]string{"total tokens", fmt.Sprintf("%d", am.TotalTokens), fmt.Sprintf("%d", bm.TotalTokens)},
		[3]string{"tokens per correct", fmt.Sprintf("%.0f", am.TokensPerCorrect), fmt.Sprintf("%.0f", bm.TokensPerCorrect)},
		[3]string{"median latency", fmt.Sprintf("%dms", am.MedianLatencyMS), fmt.Sprintf("%dms", bm.MedianLatencyMS)},
		[3]string{"mean latency", fmt.Sprintf("%.0fms", a.MeanLatencyMS), fmt.Sprintf("%.0fms", b.MeanLatencyMS)},
		[3]string{"p95 latency", fmt.Sprintf("%dms", a.P95LatencyMS), fmt.Sprintf("%dms", b.P95LatencyMS)},
		[3]string{"error-free rate", ratio(am.ErrorFree, a.Total), ratio(bm.ErrorFree, b.Total)},
	)
	if am.Consistent != nil || bm.Consistent != nil {
		rows = append(rows,
			[3]string{"consistency across runs", consistency(am), consistency(bm)},
			[3]string{"answers changed between runs", changed(am), changed(bm)},
		)
	}
	return rows
}

func omniOf(s Summary) OmniMetrics {
	if s.Omni == nil {
		return OmniMetrics{}
	}
	return *s.Omni
}

func ratio(n, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%.3f (%d/%d)", float64(n)/float64(total), n, total)
}

func consistency(m OmniMetrics) string {
	if m.Consistent == nil {
		return "n/a (1 run)"
	}
	return ratio(*m.Consistent, m.Questions)
}

func changed(m OmniMetrics) string {
	if m.AnswersChanged == nil {
		return "n/a (1 run)"
	}
	return fmt.Sprintf("%d", *m.AnswersChanged)
}

func oneLine(s string, n int) string {
	return OneLine(s, n)
}

func OneLine(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
