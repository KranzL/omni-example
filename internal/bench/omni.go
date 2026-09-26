package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type OmniMetrics struct {
	Questions          int      `json:"questions"`
	Repeats            int      `json:"repeats"`
	TotalTokens        int64    `json:"total_tokens"`
	TokensPerCorrect   float64  `json:"tokens_per_correct"`
	MedianLatencyMS    int64    `json:"median_latency_ms"`
	ErrorFree          int      `json:"error_free"`
	ErrorFreeRate      float64  `json:"error_free_rate"`
	Consistent         *int     `json:"consistent,omitempty"`
	ConsistencyRate    *float64 `json:"consistency_rate,omitempty"`
	AnswersChanged     *int     `json:"answers_changed,omitempty"`
	AnswerTextsChanged *int     `json:"answer_texts_changed,omitempty"`
}

func LineTokens(l Line) int64 {
	return l.Tokens.PromptTokens() + l.Tokens.OutputTokens + l.RouteTokens.PromptTokens() + l.RouteTokens.OutputTokens
}

func ErrorFree(l Line) bool {
	return l.Error == "" && l.SQLErrors == 0 && l.Submitted
}

func MedianMS(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

func ComputeOmni(lines []Line) OmniMetrics {
	var m OmniMetrics
	var latencies []int64
	correct := 0
	repeats := map[int]bool{}
	byID := map[string][]Line{}
	var order []string
	for _, l := range lines {
		m.TotalTokens += LineTokens(l)
		latencies = append(latencies, l.LatencyMS)
		if l.Correct {
			correct++
		}
		if ErrorFree(l) {
			m.ErrorFree++
		}
		repeats[l.Repeat] = true
		if _, ok := byID[l.ID]; !ok {
			order = append(order, l.ID)
		}
		byID[l.ID] = append(byID[l.ID], l)
	}
	m.Questions = len(order)
	m.Repeats = len(repeats)
	m.MedianLatencyMS = MedianMS(latencies)
	if correct > 0 {
		m.TokensPerCorrect = float64(m.TotalTokens) / float64(correct)
	}
	if len(lines) > 0 {
		m.ErrorFreeRate = float64(m.ErrorFree) / float64(len(lines))
	}
	if m.Repeats < 2 {
		return m
	}
	consistent, texts := 0, 0
	for _, id := range order {
		group := byID[id]
		same, sameText := true, true
		for _, l := range group[1:] {
			if l.Correct != group[0].Correct {
				same = false
			}
			if !strings.EqualFold(normalizeString(l.Answer), normalizeString(group[0].Answer)) {
				sameText = false
			}
		}
		if same {
			consistent++
		}
		if !sameText {
			texts++
		}
	}
	changed := m.Questions - consistent
	rate := float64(consistent) / float64(m.Questions)
	m.Consistent = &consistent
	m.ConsistencyRate = &rate
	m.AnswersChanged = &changed
	m.AnswerTextsChanged = &texts
	return m
}

func WithOmni(s Summary, lines []Line) Summary {
	m := ComputeOmni(lines)
	s.Omni = &m
	return s
}

func AppendOmni(summaryJSON []byte, m OmniMetrics) ([]byte, error) {
	if !json.Valid(summaryJSON) {
		return nil, fmt.Errorf("summary is not valid JSON")
	}
	block, err := json.MarshalIndent(m, "  ", "  ")
	if err != nil {
		return nil, err
	}
	body := strings.TrimRight(string(summaryJSON), " \t\r\n")
	if i := strings.LastIndex(body, ",\n  \"omni\": {"); i >= 0 {
		rest := body[i+len(",\n  \"omni\": {"):]
		if j := strings.Index(rest, "\n  }"); j >= 0 {
			out := body[:i] + ",\n  \"omni\": " + string(block) + rest[j+len("\n  }"):] + "\n"
			if !json.Valid([]byte(out)) {
				return nil, fmt.Errorf("appending omni metrics produced invalid JSON")
			}
			return []byte(out), nil
		}
		body = body[:i] + "\n}"
	}
	if !strings.HasSuffix(body, "}") {
		return nil, fmt.Errorf("summary does not end with a closing brace")
	}
	head := strings.TrimRight(strings.TrimSuffix(body, "}"), " \t\r\n")
	out := head + ",\n  \"omni\": " + string(block) + "\n}\n"
	if !json.Valid([]byte(out)) {
		return nil, fmt.Errorf("appending omni metrics produced invalid JSON")
	}
	return []byte(out), nil
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
