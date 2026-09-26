package judge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	ResultsDir = "results/judge"
	SummaryExt = ".summary.json"
	LinesExt   = ".jsonl"
)

type Line struct {
	ID                 string           `json:"id"`
	Difficulty         string           `json:"difficulty"`
	Repeat             int              `json:"repeat"`
	Config             string           `json:"config"`
	Lenient            bool             `json:"lenient_correct"`
	Strict             bool             `json:"strict_correct"`
	Answer             string           `json:"answer"`
	SQL                string           `json:"sql"`
	Expected           any              `json:"expected"`
	JudgeVerdict       string           `json:"judge_verdict"`
	JudgeConfidence    string           `json:"judge_confidence"`
	JudgeReason        string           `json:"judge_reason"`
	Supervised         bool             `json:"supervised"`
	SupervisorDecision string           `json:"supervisor_decision,omitempty"`
	SupervisorReason   string           `json:"supervisor_reason,omitempty"`
	Final              bool             `json:"final_correct"`
	RouteCostUSD       float64          `json:"route_cost_usd"`
	RouteTokens        llm.Usage        `json:"route_tokens"`
	RouteLatencyMS     int64            `json:"route_latency_ms"`
	Records            []llm.CallRecord `json:"records"`
	HumanSidesWith     string           `json:"human_sides_with,omitempty"`
	Error              string           `json:"error,omitempty"`
}

type Disagreement struct {
	ID                 string `json:"id"`
	Difficulty         string `json:"difficulty"`
	Lenient            bool   `json:"lenient_correct"`
	Strict             bool   `json:"strict_correct"`
	Judge              bool   `json:"judge_final_correct"`
	JudgeReason        string `json:"judge_reason"`
	SupervisorDecision string `json:"supervisor_decision,omitempty"`
	SupervisorReason   string `json:"supervisor_reason,omitempty"`
	Answer             string `json:"answer"`
	Expected           any    `json:"expected"`
	HumanSidesWith     string `json:"human_sides_with,omitempty"`
}

type Summary struct {
	Config           string         `json:"config"`
	Source           string         `json:"source"`
	Timestamp        string         `json:"timestamp"`
	Total            int            `json:"total"`
	Errors           int            `json:"errors"`
	JudgePass        int            `json:"judge_pass"`
	Supervised       int            `json:"supervised"`
	Overturns        int            `json:"overturns"`
	AgreeLenient     int            `json:"agree_lenient"`
	AgreeStrict      int            `json:"agree_strict"`
	AgreementLenient float64        `json:"agreement_lenient"`
	AgreementStrict  float64        `json:"agreement_strict"`
	JudgeCostUSD     float64        `json:"judge_cost_usd"`
	Disagreements    []Disagreement `json:"disagreements"`
}

func FilePath(config, stamp string) string {
	return filepath.Join(ResultsDir, config+"-"+stamp+LinesExt)
}

func SummaryPath(config, stamp string) string {
	return filepath.Join(ResultsDir, config+"-"+stamp+SummaryExt)
}

func Summarize(config, source, stamp string, lines []Line) Summary {
	s := Summary{Config: config, Source: source, Timestamp: stamp, Total: len(lines)}
	for _, l := range lines {
		s.JudgeCostUSD += l.RouteCostUSD
		if l.Error != "" {
			s.Errors++
			continue
		}
		if l.Final {
			s.JudgePass++
		}
		if l.Supervised {
			s.Supervised++
		}
		if l.SupervisorDecision == DecisionOverturn {
			s.Overturns++
		}
		lenientAgree := l.Final == l.Lenient
		strictAgree := l.Final == l.Strict
		if lenientAgree {
			s.AgreeLenient++
		}
		if strictAgree {
			s.AgreeStrict++
		}
		if !lenientAgree || !strictAgree {
			s.Disagreements = append(s.Disagreements, Disagreement{
				ID:                 l.ID,
				Difficulty:         l.Difficulty,
				Lenient:            l.Lenient,
				Strict:             l.Strict,
				Judge:              l.Final,
				JudgeReason:        l.JudgeReason,
				SupervisorDecision: l.SupervisorDecision,
				SupervisorReason:   l.SupervisorReason,
				Answer:             l.Answer,
				Expected:           l.Expected,
				HumanSidesWith:     l.HumanSidesWith,
			})
		}
	}
	if n := s.Graded(); n > 0 {
		s.AgreementLenient = float64(s.AgreeLenient) / float64(n)
		s.AgreementStrict = float64(s.AgreeStrict) / float64(n)
	}
	return s
}

func (s Summary) Graded() int {
	return s.Total - s.Errors
}

func WriteLines(path string, lines []Line) error {
	return bench.WriteJSONL(path, lines)
}

func ReadLines(path string) ([]Line, error) {
	return bench.ReadJSONL[Line](path)
}

func WriteSummary(path string, s Summary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func ReadSummary(path string) (Summary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Summary{}, err
	}
	var s Summary
	if err := json.Unmarshal(data, &s); err != nil {
		return Summary{}, err
	}
	return s, nil
}

func (s Summary) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "config=%s source=%s total=%d errors=%d judge_pass=%d\n", s.Config, s.Source, s.Total, s.Errors, s.JudgePass)
	fmt.Fprintf(&sb, "agreement_lenient=%.3f (%d/%d) agreement_strict=%.3f (%d/%d)\n",
		s.AgreementLenient, s.AgreeLenient, s.Graded(), s.AgreementStrict, s.AgreeStrict, s.Graded())
	fmt.Fprintf(&sb, "supervised=%d overturns=%d judge_cost=$%.6f\n", s.Supervised, s.Overturns, s.JudgeCostUSD)
	fmt.Fprintf(&sb, "\ndisagreements: %d\n", len(s.Disagreements))
	if len(s.Disagreements) == 0 {
		return sb.String()
	}
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tdifficulty\tlenient\tstrict\tjudge\treason\thuman")
	for _, d := range s.Disagreements {
		fmt.Fprintf(tw, "%s\t%s\t%v\t%v\t%v\t%s\t%s\n", d.ID, d.Difficulty,
			d.Lenient, d.Strict, d.Judge, bench.OneLine(d.JudgeReason, 60), d.HumanSidesWith)
	}
	tw.Flush()
	return sb.String()
}
