package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type VerdictChange struct {
	ID         string `json:"id"`
	Repeat     int    `json:"repeat"`
	Field      string `json:"field"`
	From       bool   `json:"from"`
	To         bool   `json:"to"`
	FailReason string `json:"fail_reason,omitempty"`
}

type RegradeInfo struct {
	At              string          `json:"at"`
	Lines           int             `json:"lines"`
	VerdictsChanged int             `json:"verdicts_changed"`
	StrictChanged   int             `json:"strict_changed"`
	Changes         []VerdictChange `json:"changes,omitempty"`
}

func RegradeVerdicts(q Question, l Line) (Line, []VerdictChange, error) {
	out := l
	out.Attempts = append([]Attempt(nil), l.Attempts...)
	if q.AnswerType != TypeFreeText && l.Error == "" {
		correct, err := Grade(q, l.Expected, l.Answer)
		if err != nil {
			return Line{}, nil, err
		}
		strict, err := GradeStrict(q, l.Expected, l.Answer)
		if err != nil {
			return Line{}, nil, err
		}
		out.Correct, out.CorrectStrict = correct, strict
		for i := range out.Attempts {
			a := &out.Attempts[i]
			if i == len(out.Attempts)-1 {
				a.Correct = out.Correct
				continue
			}
			if a.Error != "" {
				continue
			}
			ok, err := Grade(q, l.Expected, a.Answer)
			if err != nil {
				return Line{}, nil, err
			}
			a.Correct = ok
		}
	}
	ApplyReasons(q, &out)
	var changes []VerdictChange
	if out.Correct != l.Correct {
		changes = append(changes, VerdictChange{ID: l.ID, Repeat: l.Repeat, Field: "correct", From: l.Correct, To: out.Correct, FailReason: out.FailReason})
	}
	if out.CorrectStrict != l.CorrectStrict {
		changes = append(changes, VerdictChange{ID: l.ID, Repeat: l.Repeat, Field: "correct_strict", From: l.CorrectStrict, To: out.CorrectStrict, FailReason: out.FailReason})
	}
	return out, changes, nil
}

func setRawBool(s string, from int, key string, v bool) (string, int, error) {
	anchor := `"` + key + `":`
	i := strings.Index(s[from:], anchor)
	if i < 0 {
		return s, -1, nil
	}
	j := from + i + len(anchor)
	switch {
	case strings.HasPrefix(s[j:], "true"):
		s = s[:j] + fmt.Sprint(v) + s[j+4:]
	case strings.HasPrefix(s[j:], "false"):
		s = s[:j] + fmt.Sprint(v) + s[j+5:]
	default:
		return "", 0, fmt.Errorf("%s is not a bool", key)
	}
	return s, j + len(fmt.Sprint(v)), nil
}

func PatchRawVerdicts(raw []byte, l Line) ([]byte, error) {
	s := string(raw)
	s, end, err := setRawBool(s, 0, "correct", l.Correct)
	if err != nil {
		return nil, err
	}
	if end < 0 {
		return nil, fmt.Errorf("line has no correct field")
	}
	s, strictEnd, err := setRawBool(s, 0, "correct_strict", l.CorrectStrict)
	if err != nil {
		return nil, err
	}
	if strictEnd < 0 {
		s = s[:end] + fmt.Sprintf(`,"correct_strict":%t`, l.CorrectStrict) + s[end:]
	}
	if len(l.Attempts) > 0 {
		at := strings.Index(s, `"attempts":[`)
		if at < 0 {
			return nil, fmt.Errorf("line has attempts but no attempts array")
		}
		pos := at
		for i, a := range l.Attempts {
			var next int
			if s, next, err = setRawBool(s, pos, "correct", a.Correct); err != nil {
				return nil, fmt.Errorf("attempt %d: %w", i+1, err)
			}
			if next < 0 {
				return nil, fmt.Errorf("attempt %d has no correct field", i+1)
			}
			pos = next
		}
	}
	return AnnotateRawLine([]byte(s), l.FailReason, l.FormatReason)
}

func RegradeVerdictsFile(path string, qs []Question) ([]Line, []VerdictChange, error) {
	byID := make(map[string]Question, len(qs))
	for _, q := range qs {
		byID[q.ID] = q
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	raws := splitJSONLines(string(data))
	lines := make([]Line, len(raws))
	var changes []VerdictChange
	var sb strings.Builder
	for i, raw := range raws {
		var l Line
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if !strings.Contains(raw, `"correct_strict":`) {
			l.CorrectStrict = l.Correct
		}
		q, ok := byID[l.ID]
		if !ok {
			return nil, nil, fmt.Errorf("line %d: unknown question %q", i+1, l.ID)
		}
		out, ch, err := RegradeVerdicts(q, l)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		patched, err := PatchRawVerdicts([]byte(raw), out)
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		var check Line
		if err := json.Unmarshal(patched, &check); err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if check.Answer != l.Answer || check.Correct != out.Correct || check.CorrectStrict != out.CorrectStrict || check.FailReason != out.FailReason || check.FormatReason != out.FormatReason {
			return nil, nil, fmt.Errorf("line %d: patched line does not round-trip", i+1)
		}
		lines[i] = out
		changes = append(changes, ch...)
		sb.Write(patched)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return nil, nil, err
	}
	return lines, changes, nil
}

func ResummarizeVerdicts(old Summary, lines []Line, info RegradeInfo) Summary {
	fresh := Summarize(old.Config, old.Timestamp, old.Source, old.Repeat, lines)
	s := old
	s.Total = fresh.Total
	s.Correct = fresh.Correct
	s.Accuracy = fresh.Accuracy
	s.CorrectStrict = fresh.CorrectStrict
	s.AccuracyStrict = fresh.AccuracyStrict
	s.ByDifficulty = fresh.ByDifficulty
	s.FailReasons = fresh.FailReasons
	s.FormatReasons = fresh.FormatReasons
	s.CostPerCorrectUSD = fresh.CostPerCorrectUSD
	if old.Escalation != nil || old.Verifier != nil {
		s.Escalation = fresh.Escalation
		s.Verifier = fresh.Verifier
	}
	if old.Omni != nil {
		m := ComputeOmni(lines)
		s.Omni = &m
	}
	s.Regraded = &info
	return s
}
