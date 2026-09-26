package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	SetFull    = "full"
	SetStarter = "starter"
)

type SetEntry struct {
	ID  string `yaml:"id"`
	Why string `yaml:"why"`
}

type QuestionSet struct {
	Name      string     `yaml:"name"`
	Note      string     `yaml:"note"`
	Questions []SetEntry `yaml:"questions"`
}

func SetPath(benchDir, name string) string {
	return filepath.Join(benchDir, name+".yaml")
}

func LoadSet(path string) (QuestionSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return QuestionSet{}, err
	}
	var s QuestionSet
	if err := yaml.Unmarshal(data, &s); err != nil {
		return QuestionSet{}, err
	}
	if len(s.Questions) == 0 {
		return QuestionSet{}, fmt.Errorf("%s: set has no questions", path)
	}
	return s, nil
}

func SelectSet(qs []Question, set QuestionSet) ([]Question, error) {
	byID := make(map[string]Question, len(qs))
	for _, q := range qs {
		byID[q.ID] = q
	}
	seen := map[string]bool{}
	out := make([]Question, 0, len(set.Questions))
	for _, e := range set.Questions {
		q, ok := byID[e.ID]
		if !ok {
			return nil, fmt.Errorf("set %s names unknown question %q", set.Name, e.ID)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("set %s names %q twice", set.Name, e.ID)
		}
		seen[e.ID] = true
		out = append(out, q)
	}
	return out, nil
}

func Regrade(qs []Question, lines []Line) ([]Line, error) {
	byID := make(map[string]Question, len(qs))
	for _, q := range qs {
		byID[q.ID] = q
	}
	out := make([]Line, len(lines))
	for i, l := range lines {
		q, ok := byID[l.ID]
		if !ok {
			return nil, fmt.Errorf("line %d: unknown question %q", i+1, l.ID)
		}
		ApplyReasons(q, &l)
		out[i] = l
	}
	return out, nil
}

func passes(o QuestionOutcome) bool {
	return o.Total > 0 && o.Correct*2 > o.Total
}

type RegressFlip struct {
	ID         string
	Difficulty string
	Baseline   QuestionOutcome
	Candidate  QuestionOutcome
	Reason     string
}

const ReasonMissingFromCandidate = "missing from candidate"

type Regression struct {
	Baseline      Side
	Candidate     Side
	Common        []string
	Missing       []string
	BaselinePass  int
	CandidatePass int
	PassToFail    []RegressFlip
	FailToPass    []RegressFlip
	MaxDrop       int
	MaxFlips      int
}

func Regress(base, cand Side, maxDrop, maxFlips int) Regression {
	bo, bdiff, border := outcomes(base.Lines)
	co, _, _ := outcomes(cand.Lines)
	r := Regression{Baseline: base, Candidate: cand, MaxDrop: maxDrop, MaxFlips: maxFlips}
	for _, id := range border {
		b := bo[id]
		c, ok := co[id]
		if !ok {
			r.Missing = append(r.Missing, id)
			if passes(b) {
				r.PassToFail = append(r.PassToFail, RegressFlip{ID: id, Difficulty: bdiff[id], Baseline: b, Reason: ReasonMissingFromCandidate})
			}
			continue
		}
		r.Common = append(r.Common, id)
		bp, cp := passes(b), passes(c)
		if bp {
			r.BaselinePass++
		}
		if cp {
			r.CandidatePass++
		}
		f := RegressFlip{ID: id, Difficulty: bdiff[id], Baseline: b, Candidate: c}
		if bp && !cp {
			r.PassToFail = append(r.PassToFail, f)
		}
		if !bp && cp {
			r.FailToPass = append(r.FailToPass, f)
		}
	}
	return r
}

func (r Regression) Drop() int {
	return r.BaselinePass - r.CandidatePass
}

func (r Regression) Failures() []string {
	var out []string
	if len(r.Common) == 0 {
		out = append(out, "no questions in common")
	}
	if d := r.Drop(); d > r.MaxDrop {
		out = append(out, fmt.Sprintf("accuracy dropped by %d shared questions (max %d)", d, r.MaxDrop))
	}
	if n := len(r.PassToFail); n > r.MaxFlips {
		out = append(out, fmt.Sprintf("pass-to-fail flips %d (max %d)", n, r.MaxFlips))
	}
	return out
}

func (r Regression) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "baseline:  %s (%s)\ncandidate: %s (%s)\n", r.Baseline.Label, r.Baseline.Path, r.Candidate.Label, r.Candidate.Path)
	fmt.Fprintf(&sb, "questions in common: %d (baseline %d, candidate %d)\n", len(r.Common), countIDs(r.Baseline.Lines), countIDs(r.Candidate.Lines))
	fmt.Fprintf(&sb, "passing: baseline %d/%d, candidate %d/%d, drop %d (max %d)\n", r.BaselinePass, len(r.Common), r.CandidatePass, len(r.Common), r.Drop(), r.MaxDrop)
	fmt.Fprintf(&sb, "pass to fail: %d (max %d)\nfail to pass: %d\n", len(r.PassToFail), r.MaxFlips, len(r.FailToPass))
	if len(r.Missing) > 0 {
		fmt.Fprintf(&sb, "missing from candidate: %d (%s), baseline passes among them count as pass to fail\n", len(r.Missing), strings.Join(r.Missing, ", "))
	}
	writeFlips(&sb, "pass to fail", r.PassToFail)
	writeFlips(&sb, "fail to pass", r.FailToPass)
	if f := r.Failures(); len(f) > 0 {
		fmt.Fprintf(&sb, "result: FAIL, %s\n", strings.Join(f, "; "))
	} else {
		fmt.Fprintf(&sb, "result: PASS\n")
	}
	return sb.String()
}

func writeFlips(sb *strings.Builder, title string, flips []RegressFlip) {
	if len(flips) == 0 {
		return
	}
	fmt.Fprintf(sb, "\n%s:\n", title)
	for _, f := range flips {
		fmt.Fprintf(sb, "%s (%s)\n", f.ID, f.Difficulty)
		fmt.Fprintf(sb, "  baseline:  %s\n", outcomeDetail(f.Baseline))
		if f.Reason != "" {
			fmt.Fprintf(sb, "  candidate: %s\n", f.Reason)
			continue
		}
		fmt.Fprintf(sb, "  candidate: %s\n", outcomeDetail(f.Candidate))
	}
}

func outcomeDetail(o QuestionOutcome) string {
	if passes(o) {
		return fmt.Sprintf("%s answer %q", o.Verdict(), oneLine(o.Answer, 80))
	}
	out := fmt.Sprintf("%s answer %q", o.Verdict(), oneLine(o.FailAnswer, 80))
	if o.FailReason != "" {
		out += ", reason " + o.FailReason
	}
	return out
}

func countIDs(lines []Line) int {
	seen := map[string]bool{}
	for _, l := range lines {
		seen[l.ID] = true
	}
	return len(seen)
}

type DriftChange struct {
	ID     string
	From   string
	To     string
	Reason string
}

type DriftRun struct {
	Path      string
	Stamp     string
	Questions int
	Total     int
	Correct   int
	Strict    int
	CostUSD   float64
	Added     int
	Removed   int
	Changes   []DriftChange
}

func Drift(sides []Side) []DriftRun {
	sorted := append([]Side(nil), sides...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Summary.Timestamp < sorted[j].Summary.Timestamp })
	var out []DriftRun
	var prev map[string]QuestionOutcome
	for _, s := range sorted {
		cur, _, order := outcomes(s.Lines)
		run := DriftRun{
			Path:      s.Path,
			Stamp:     s.Summary.Timestamp,
			Questions: len(order),
			Total:     s.Summary.Total,
			Correct:   s.Summary.Correct,
			Strict:    s.Summary.CorrectStrict,
			CostUSD:   s.Summary.TotalCostUSD + s.Summary.JudgeCostUSD,
		}
		if prev != nil {
			for _, id := range order {
				p, ok := prev[id]
				if !ok {
					run.Added++
					continue
				}
				c := cur[id]
				if passes(p) == passes(c) {
					continue
				}
				ch := DriftChange{ID: id, From: verdictWord(p), To: verdictWord(c)}
				if !passes(c) {
					ch.Reason = c.FailReason
				}
				run.Changes = append(run.Changes, ch)
			}
			for id := range prev {
				if _, ok := cur[id]; !ok {
					run.Removed++
				}
			}
		}
		out = append(out, run)
		prev = cur
	}
	return out
}

func verdictWord(o QuestionOutcome) string {
	if passes(o) {
		return "pass"
	}
	return "fail"
}

func FormatDrift(label string, runs []DriftRun) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "drift for %s: %d runs\n\n", label, len(runs))
	for i, r := range runs {
		acc := 0.0
		if r.Total > 0 {
			acc = float64(r.Correct) / float64(r.Total)
		}
		fmt.Fprintf(&sb, "%s  questions=%d accuracy=%.3f (%d/%d) strict=%d cost=$%.6f", r.Stamp, r.Questions, acc, r.Correct, r.Total, r.Strict, r.CostUSD)
		if i > 0 {
			fmt.Fprintf(&sb, " changed=%d", len(r.Changes))
			if r.Added > 0 {
				fmt.Fprintf(&sb, " added=%d", r.Added)
			}
			if r.Removed > 0 {
				fmt.Fprintf(&sb, " removed=%d", r.Removed)
			}
		}
		fmt.Fprintf(&sb, "  %s\n", r.Path)
		for _, c := range r.Changes {
			fmt.Fprintf(&sb, "    %s %s -> %s", c.ID, c.From, c.To)
			if c.Reason != "" {
				fmt.Fprintf(&sb, ": %s", c.Reason)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func AnnotateRawLine(raw []byte, failReason, formatReason string) ([]byte, error) {
	s := string(raw)
	for {
		members, err := rawMembers(s)
		if err != nil {
			return nil, err
		}
		k := slices.IndexFunc(members, func(m rawMember) bool { return m.key == "fail_reason" || m.key == "format_reason" })
		if k < 0 {
			break
		}
		s = removeRawMember(s, members, k)
	}
	members, err := rawMembers(s)
	if err != nil {
		return nil, err
	}
	k := slices.IndexFunc(members, func(m rawMember) bool { return m.key == "correct_strict" })
	if k < 0 {
		k = slices.IndexFunc(members, func(m rawMember) bool { return m.key == "correct" })
	}
	if k < 0 {
		return nil, fmt.Errorf("line has no correct or correct_strict field")
	}
	if v := s[members[k].valueStart:members[k].end]; v != "true" && v != "false" {
		return nil, fmt.Errorf("%s is not a bool", members[k].key)
	}
	var ins strings.Builder
	for _, kv := range [][2]string{{"fail_reason", failReason}, {"format_reason", formatReason}} {
		if kv[1] == "" {
			continue
		}
		v, err := json.Marshal(kv[1])
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&ins, `,"%s":%s`, kv[0], v)
	}
	j := members[k].end
	out := s[:j] + ins.String() + s[j:]
	if !json.Valid([]byte(out)) {
		return nil, fmt.Errorf("annotated line is not valid JSON")
	}
	return []byte(out), nil
}

type rawMember struct {
	key        string
	start      int
	valueStart int
	end        int
}

func rawMembers(s string) ([]rawMember, error) {
	i := skipJSONSpace(s, 0)
	if i >= len(s) || s[i] != '{' {
		return nil, fmt.Errorf("line is not a JSON object")
	}
	i = skipJSONSpace(s, i+1)
	var out []rawMember
	if i < len(s) && s[i] == '}' {
		return out, nil
	}
	for {
		m := rawMember{start: i}
		dec := json.NewDecoder(strings.NewReader(s[i:]))
		if err := dec.Decode(&m.key); err != nil {
			return nil, fmt.Errorf("object key: %w", err)
		}
		i = skipJSONSpace(s, i+int(dec.InputOffset()))
		if i >= len(s) || s[i] != ':' {
			return nil, fmt.Errorf("%s: missing colon", m.key)
		}
		i = skipJSONSpace(s, i+1)
		m.valueStart = i
		dec = json.NewDecoder(strings.NewReader(s[i:]))
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%s: %w", m.key, err)
		}
		m.end = i + int(dec.InputOffset())
		out = append(out, m)
		i = skipJSONSpace(s, m.end)
		if i >= len(s) {
			return nil, fmt.Errorf("unterminated object")
		}
		switch s[i] {
		case ',':
			i = skipJSONSpace(s, i+1)
		case '}':
			return out, nil
		default:
			return nil, fmt.Errorf("unexpected %q after %s", s[i], m.key)
		}
	}
}

func removeRawMember(s string, members []rawMember, k int) string {
	switch {
	case k > 0:
		return s[:members[k-1].end] + s[members[k].end:]
	case len(members) > 1:
		return s[:members[0].start] + s[members[1].start:]
	default:
		return s[:members[0].start] + s[members[0].end:]
	}
}

func skipJSONSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	return i
}

func RegradeFile(path string, qs []Question) ([]Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raws := splitJSONLines(string(data))
	lines := make([]Line, len(raws))
	for i, raw := range raws {
		l, err := DecodeLine(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		lines[i] = l
	}
	out, err := Regrade(qs, lines)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	for i, raw := range raws {
		annotated, err := AnnotateRawLine([]byte(raw), out[i].FailReason, out[i].FormatReason)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		sb.Write(annotated)
		sb.WriteByte('\n')
	}
	if err := replaceFile(path, []byte(sb.String())); err != nil {
		return nil, err
	}
	return out, nil
}

func replaceFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp, 0o644)
	}
	if werr == nil {
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	return nil
}

var summaryReasonBlock = regexp.MustCompile(`,\n  "(fail_reasons|format_reasons)": \{[^}]*\}`)

func SetSummaryReasons(summaryJSON []byte, fail, format map[string]int) ([]byte, error) {
	if !json.Valid(summaryJSON) {
		return nil, fmt.Errorf("summary is not valid JSON")
	}
	body := strings.TrimRight(string(summaryJSON), " \t\r\n")
	body = summaryReasonBlock.ReplaceAllString(body, "")
	var ins strings.Builder
	for _, kv := range []struct {
		key    string
		counts map[string]int
	}{{"fail_reasons", fail}, {"format_reasons", format}} {
		if len(kv.counts) == 0 {
			continue
		}
		block, err := json.MarshalIndent(kv.counts, "  ", "  ")
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&ins, ",\n  %q: %s", kv.key, block)
	}
	at := strings.LastIndex(body, ",\n  \"omni\": {")
	if at < 0 {
		if !strings.HasSuffix(body, "}") {
			return nil, fmt.Errorf("summary does not end with a closing brace")
		}
		at = len(strings.TrimRight(strings.TrimSuffix(body, "}"), " \t\r\n"))
	}
	out := body[:at] + ins.String() + body[at:]
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if !json.Valid([]byte(out)) {
		return nil, fmt.Errorf("adding reason counts produced invalid JSON")
	}
	return []byte(out), nil
}
