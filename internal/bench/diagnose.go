package bench

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	ClassWrongFilter   = "wrong_filter"
	ClassWrongWindow   = "wrong_window"
	ClassWrongGrain    = "wrong_grain"
	ClassMissingThresh = "missing_threshold"
	ClassFormat        = "format"
	ClassAgentError    = "agent_error"
	ClassUnclassified  = "unclassified"
)

type Diagnosis struct {
	ID           string
	Difficulty   string
	Repeat       int
	Question     string
	AnswerType   string
	Expected     any
	GroundSQL    string
	AgentSQL     string
	AgentAnswer  string
	Lenient      bool
	Strict       bool
	FailReason   string
	FormatReason string
	Class        string
	Detail       string
}

type StaleLine struct {
	ID     string
	Repeat int
	Note   string
}

type DiagnoseOutcome struct {
	Lines      []Diagnosis
	Stale      []StaleLine
	Total      int
	Wrong      int
	StrictOnly int
}

func Diagnose(lines []Line, byID map[string]Question, strict bool) (DiagnoseOutcome, error) {
	out := DiagnoseOutcome{Total: len(lines)}
	for _, l := range lines {
		q, ok := byID[l.ID]
		if !ok {
			return out, fmt.Errorf("line %s rep=%d: unknown question id", l.ID, l.Repeat)
		}
		lenient, strictOK, err := currentVerdicts(q, l)
		if err != nil {
			return out, fmt.Errorf("line %s rep=%d: %w", l.ID, l.Repeat, err)
		}
		if !l.Correct && lenient {
			if strictOK || !strict {
				out.Stale = append(out.Stale, StaleLine{ID: l.ID, Repeat: l.Repeat,
					Note: "recorded fail, current grader passes; skipped"})
				continue
			}
			out.Stale = append(out.Stale, StaleLine{ID: l.ID, Repeat: l.Repeat,
				Note: "recorded fail, current lenient grader passes and strict fails; treated as strict-only"})
		}
		if !lenient {
			out.Wrong++
			class, detail := Classify(q, l, lenient)
			out.Lines = append(out.Lines, Diagnosis{
				ID: l.ID, Difficulty: l.Difficulty, Repeat: l.Repeat,
				Question: q.Text, AnswerType: q.AnswerType, Expected: l.Expected,
				GroundSQL: q.SQL, AgentSQL: l.SQL, AgentAnswer: l.Answer,
				Lenient: lenient, Strict: strictOK,
				FailReason: l.FailReason, FormatReason: l.FormatReason,
				Class: class, Detail: detail,
			})
			continue
		}
		if !strictOK {
			if !strict {
				continue
			}
			if l.CorrectStrict {
				out.Stale = append(out.Stale, StaleLine{ID: l.ID, Repeat: l.Repeat,
					Note: "recorded strict pass, current strict grader fails; treated as wrong"})
			}
			out.StrictOnly++
			class, detail := Classify(q, l, lenient)
			out.Lines = append(out.Lines, Diagnosis{
				ID: l.ID, Difficulty: l.Difficulty, Repeat: l.Repeat,
				Question: q.Text, AnswerType: q.AnswerType, Expected: l.Expected,
				GroundSQL: q.SQL, AgentSQL: l.SQL, AgentAnswer: l.Answer,
				Lenient: lenient, Strict: strictOK,
				FailReason: l.FailReason, FormatReason: l.FormatReason,
				Class: class, Detail: detail,
			})
		}
	}
	return out, nil
}

func currentVerdicts(q Question, l Line) (bool, bool, error) {
	if q.AnswerType == TypeFreeText {
		return l.Correct, l.CorrectStrict, nil
	}
	lenient, err := Grade(q, l.Expected, l.Answer)
	if err != nil {
		return false, false, err
	}
	strictOK, err := GradeStrict(q, l.Expected, l.Answer)
	if err != nil {
		return false, false, err
	}
	return lenient, strictOK, nil
}

func Classify(q Question, l Line, lenient bool) (string, string) {
	if l.Error != "" || !l.Submitted || strings.TrimSpace(l.SQL) == "" {
		why := l.Error
		if why == "" {
			why = l.Failure
		}
		if why == "" {
			why = "agent submitted no SQL"
		}
		return ClassAgentError, oneLine(why, 160)
	}
	if lenient {
		reason := l.FormatReason
		if reason == "" {
			reason = FormatReason(q, l.Expected, l.Answer)
		}
		return ClassFormat, "lenient passes, strict fails: " + oneLine(reason, 160)
	}
	ground := normalizeSQL(q.SQL)
	agent := normalizeSQL(l.SQL)
	if ground == agent {
		return ClassFormat, "SQL matches ground truth; answer text differs"
	}
	if q.AnswerType == TypeTable && tableValuesMatch(l.Expected, l.Answer, q.EffectiveTolerance()) {
		return ClassFormat, "row values match expected; labels or shape differ"
	}
	if class, detail, ok := classifyFilter(q.SQL, l.SQL); ok {
		return class, detail
	}
	if class, detail, ok := classifyWindow(q.SQL, l.SQL); ok {
		return class, detail
	}
	if class, detail, ok := classifyGrain(q.SQL, l.SQL); ok {
		return class, detail
	}
	if class, detail, ok := classifyThreshold(q.SQL, l.SQL); ok {
		return class, detail
	}
	if orderDiffers(q.SQL, l.SQL) {
		return ClassUnclassified, "ORDER BY differs: ground " + oneLine(orderClause(q.SQL), 80) + " vs agent " + oneLine(orderClause(l.SQL), 80)
	}
	return ClassUnclassified, "SQL differs outside filter, window, grain, and threshold features"
}

var spacePattern = regexp.MustCompile(`\s+`)

func normalizeSQL(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ";")
	return spacePattern.ReplaceAllString(s, " ")
}

type valueRow struct {
	label string
	nums  []float64
}

func parseNumbers(fields []string) []float64 {
	var out []float64
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if idx := strings.Index(field, "="); idx >= 0 {
			field = field[idx+1:]
		}
		if v, err := strconv.ParseFloat(field, 64); err == nil {
			out = append(out, v)
		}
	}
	return out
}

func splitValueFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == ' ' || r == '\t'
	})
}

func gotValueRow(line string) valueRow {
	if label, rest, ok := cutLenientTableLine(line); ok {
		return valueRow{label: label, nums: parseNumbers(splitValueFields(rest))}
	}
	fields := strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == '\t'
	})
	if len(fields) < 2 {
		fields = strings.Fields(line)
	}
	if len(fields) == 0 {
		return valueRow{}
	}
	return valueRow{label: strings.TrimSpace(fields[0]), nums: parseNumbers(fields[1:])}
}

func labelsMatch(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == "" || b == "" {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	if !strings.HasPrefix(b, a) {
		return false
	}
	if len(b) == len(a) {
		return true
	}
	next := b[len(a)]
	return !(next >= '0' && next <= '9' || next >= 'a' && next <= 'z')
}

func tableValuesMatch(expected any, got string, tol float64) bool {
	want, err := expectedTable(expected)
	if err != nil {
		return false
	}
	if len(want.Columns) <= 1 {
		return false
	}
	var wantRows []valueRow
	for _, row := range want.Rows {
		if len(row) < 2 {
			continue
		}
		if nums := parseNumbers(row[1:]); len(nums) > 0 {
			wantRows = append(wantRows, valueRow{label: row[0], nums: nums})
		}
	}
	var gotRows []valueRow
	for _, line := range SplitLines(got) {
		if r := gotValueRow(line); len(r.nums) > 0 {
			gotRows = append(gotRows, r)
		}
	}
	if len(wantRows) == 0 || len(gotRows) != len(wantRows) {
		return false
	}
	used := make([]bool, len(gotRows))
	for _, w := range wantRows {
		found := false
		for i, g := range gotRows {
			if used[i] || !labelsMatch(w.label, g.label) || !numsMatch(w.nums, g.nums, tol) {
				continue
			}
			used[i] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func numsMatch(want, got []float64, tol float64) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if !WithinTolerance(got[i], want[i], tol) {
			return false
		}
	}
	return true
}

var (
	statusExcludePattern = regexp.MustCompile(`(?i)status\s*(?:<>|!=)\s*'([^']+)'`)
	statusNotInPattern   = regexp.MustCompile(`(?i)status\s+not\s+in\s*\(([^)]*)\)`)
	statusInPattern      = regexp.MustCompile(`(?i)status\s+in\s*\(([^)]*)\)`)
	statusEqPattern      = regexp.MustCompile(`(?i)status\s*=\s*'([^']+)'`)
	caseStatusPattern    = regexp.MustCompile(`(?i)case\s+when\s+status`)
	whereStatusPattern   = regexp.MustCompile(`(?i)where\b[^;]*\bstatus\b`)
	literalEqPattern     = regexp.MustCompile(`(?i)\b(traffic_source|event_type|category|department|product_category|product_department|browser|os|gender)\s*(?:=|in\b)\s*('[^']+'|\([^)]*\))`)
)

func literalSet(fragment string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(fragment, -1) {
		out[m[1]] = true
	}
	return out
}

func statusExcluded(sql string) map[string]bool {
	out := map[string]bool{}
	for _, m := range statusExcludePattern.FindAllStringSubmatch(sql, -1) {
		out[m[1]] = true
	}
	for _, m := range statusNotInPattern.FindAllStringSubmatch(sql, -1) {
		for lit := range literalSet(m[1]) {
			out[lit] = true
		}
	}
	return out
}

func statusIncluded(sql string) map[string]bool {
	out := map[string]bool{}
	for _, m := range statusEqPattern.FindAllStringSubmatch(sql, -1) {
		out[m[1]] = true
	}
	for _, m := range statusInPattern.FindAllStringSubmatch(sql, -1) {
		for lit := range literalSet(m[1]) {
			out[lit] = true
		}
	}
	return out
}

func sortedKeys(set map[string]bool) []string {
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func classifyFilter(groundSQL, agentSQL string) (string, string, bool) {
	ge, ae := statusExcluded(groundSQL), statusExcluded(agentSQL)
	if !sameSet(ge, ae) {
		return ClassWrongFilter, fmt.Sprintf("status excluded: ground [%s], agent [%s]",
			strings.Join(sortedKeys(ge), ", "), strings.Join(sortedKeys(ae), ", ")), true
	}
	gi, ai := statusIncluded(groundSQL), statusIncluded(agentSQL)
	if !sameSet(gi, ai) {
		return ClassWrongFilter, fmt.Sprintf("status included: ground [%s], agent [%s]",
			strings.Join(sortedKeys(gi), ", "), strings.Join(sortedKeys(ai), ", ")), true
	}
	if whereStatusPattern.MatchString(groundSQL) && caseStatusPattern.MatchString(agentSQL) && !whereStatusPattern.MatchString(agentSQL) {
		return ClassWrongFilter, "agent aggregates with CASE WHEN status where ground truth filters rows with WHERE", true
	}
	gm := literalEqPattern.FindAllStringSubmatch(groundSQL, -1)
	am := literalEqPattern.FindAllStringSubmatch(agentSQL, -1)
	gl, al := map[string]bool{}, map[string]bool{}
	gd, ad := map[string]string{}, map[string]string{}
	for _, m := range gm {
		key := strings.ToLower(m[1] + "=" + m[2])
		gl[key] = true
		gd[key] = m[1] + "=" + m[2]
	}
	for _, m := range am {
		key := strings.ToLower(m[1] + "=" + m[2])
		al[key] = true
		ad[key] = m[1] + "=" + m[2]
	}
	if !sameSet(gl, al) {
		var gs, as []string
		for _, k := range sortedKeys(gl) {
			gs = append(gs, gd[k])
		}
		for _, k := range sortedKeys(al) {
			as = append(as, ad[k])
		}
		return ClassWrongFilter, fmt.Sprintf("dimension filter: ground [%s], agent [%s]",
			strings.Join(gs, ", "), strings.Join(as, ", ")), true
	}
	return "", "", false
}

const (
	dateColumns = `(created_at|sold_at|shipped_at|delivered_at|returned_at)`
	dateLiteral = `(?:(?:date|timestamp(?:tz)?|timestamp\s+with(?:out)?\s+time\s+zone)\s+)?'(\d{4}-\d{2}-\d{2})(?:[ t](\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?))?'`
)

var (
	dateComparePattern = regexp.MustCompile(`(?i)` + dateColumns + `\s*(>=|<=|>|<)\s*` + dateLiteral)
	dateBetweenPattern = regexp.MustCompile(`(?i)` + dateColumns + `\s+between\s+` + dateLiteral + `\s+and\s+` + dateLiteral)
	zeroTimePattern    = regexp.MustCompile(`^00:00(?::00(?:\.0+)?)?$`)
)

func boundDate(date, clock string) string {
	if clock == "" || zeroTimePattern.MatchString(clock) {
		return date
	}
	return date + " " + clock
}

type windowBound struct {
	column string
	op     string
	date   string
}

func windowBounds(sql string) []windowBound {
	var out []windowBound
	for _, m := range dateComparePattern.FindAllStringSubmatch(sql, -1) {
		out = append(out, windowBound{strings.ToLower(m[1]), m[2], boundDate(m[3], m[4])})
	}
	for _, m := range dateBetweenPattern.FindAllStringSubmatch(sql, -1) {
		col := strings.ToLower(m[1])
		out = append(out, windowBound{col, ">=", boundDate(m[2], m[3])}, windowBound{col, "<=", boundDate(m[4], m[5])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].column != out[j].column {
			return out[i].column < out[j].column
		}
		if out[i].op != out[j].op {
			return out[i].op < out[j].op
		}
		return out[i].date < out[j].date
	})
	return out
}

func formatBounds(bounds []windowBound) string {
	parts := make([]string, len(bounds))
	for i, b := range bounds {
		parts[i] = b.column + " " + b.op + " '" + b.date + "'"
	}
	return strings.Join(parts, ", ")
}

func classifyWindow(groundSQL, agentSQL string) (string, string, bool) {
	g, a := windowBounds(groundSQL), windowBounds(agentSQL)
	if len(g) == 0 && len(a) == 0 {
		return "", "", false
	}
	gs, as := formatBounds(g), formatBounds(a)
	if gs == as {
		return "", "", false
	}
	if len(g) == 2 && len(a) == 1 {
		return ClassWrongWindow, "one-sided window: ground [" + gs + "], agent [" + as + "]", true
	}
	return ClassWrongWindow, "window bounds: ground [" + gs + "], agent [" + as + "]", true
}

var (
	groupByPattern       = regexp.MustCompile(`(?is)group\s+by\s+(.*?)(?:\s+having\b|\s+order\b|\s+limit\b|\s*$)`)
	distinctOrderPattern = regexp.MustCompile(`(?i)count\s*\(\s*distinct\s+order_id\s*\)`)
	minCreatedPattern    = regexp.MustCompile(`(?i)min\s*\(\s*\w*\.?created_at\s*\)`)
	rowNumberPattern     = regexp.MustCompile(`(?i)row_number\s*\(\s*\)`)
	knownTablePattern    = regexp.MustCompile(`(?i)\b(users|order_items|events|inventory_items|products|distribution_centers)\b`)
	limitPattern         = regexp.MustCompile(`(?is)\blimit\s+(\d+)`)
	havingPattern        = regexp.MustCompile(`(?i)\bhaving\b`)
	clauseEndPattern     = regexp.MustCompile(`(?i)^\s+(?:order|limit|union|intersect|except|window)\b`)
	percentilePattern    = regexp.MustCompile(`(?i)percentile_cont\s*\(\s*([0-9.]+)\s*\)`)
	orderByPattern       = regexp.MustCompile(`(?is)\border\s+by\b(.*?)(?:\s+limit\b|\s*$)`)
)

func groupKeys(sql string) []string {
	m := groupByPattern.FindStringSubmatch(sql)
	if m == nil {
		return nil
	}
	var out []string
	for _, part := range strings.Split(m[1], ",") {
		if k := spacePattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(part)), " "); k != "" {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func tableSet(sql string) map[string]bool {
	out := map[string]bool{}
	for _, m := range knownTablePattern.FindAllStringSubmatch(sql, -1) {
		out[strings.ToLower(m[1])] = true
	}
	return out
}

func classifyGrain(groundSQL, agentSQL string) (string, string, bool) {
	if minCreatedPattern.MatchString(groundSQL) && rowNumberPattern.MatchString(groundSQL) &&
		rowNumberPattern.MatchString(agentSQL) && !minCreatedPattern.MatchString(agentSQL) {
		return ClassWrongGrain, "agent ranks raw items where ground truth ranks orders by MIN(created_at)", true
	}
	gk, ak := groupKeys(groundSQL), groupKeys(agentSQL)
	if strings.Join(gk, "|") != strings.Join(ak, "|") {
		return ClassWrongGrain, fmt.Sprintf("GROUP BY: ground [%s], agent [%s]",
			oneLine(strings.Join(gk, ", "), 120), oneLine(strings.Join(ak, ", "), 120)), true
	}
	gd, ad := distinctOrderPattern.MatchString(groundSQL), distinctOrderPattern.MatchString(agentSQL)
	if gd != ad {
		if gd {
			return ClassWrongGrain, "ground truth counts DISTINCT order_id, agent does not", true
		}
		return ClassWrongGrain, "agent counts DISTINCT order_id, ground truth does not", true
	}
	gt, at := tableSet(groundSQL), tableSet(agentSQL)
	if !sameSet(gt, at) {
		return ClassWrongGrain, fmt.Sprintf("tables: ground [%s], agent [%s]",
			strings.Join(sortedKeys(gt), ", "), strings.Join(sortedKeys(at), ", ")), true
	}
	return "", "", false
}

func classifyThreshold(groundSQL, agentSQL string) (string, string, bool) {
	gl, al := limitPattern.FindStringSubmatch(groundSQL), limitPattern.FindStringSubmatch(agentSQL)
	if (gl == nil) != (al == nil) || (gl != nil && al != nil && gl[1] != al[1]) {
		gs, as := "none", "none"
		if gl != nil {
			gs = gl[1]
		}
		if al != nil {
			as = al[1]
		}
		return ClassMissingThresh, "LIMIT: ground " + gs + ", agent " + as, true
	}
	ghs, ahs := havingClause(groundSQL), havingClause(agentSQL)
	if ghs != ahs {
		return ClassMissingThresh, "HAVING: ground [" + oneLine(ghs, 80) + "], agent [" + oneLine(ahs, 80) + "]", true
	}
	gp, ap := percentilePattern.FindStringSubmatch(groundSQL), percentilePattern.FindStringSubmatch(agentSQL)
	if (gp == nil) != (ap == nil) || (gp != nil && ap != nil && gp[1] != ap[1]) {
		gs, as := "none", "none"
		if gp != nil {
			gs = gp[1]
		}
		if ap != nil {
			as = ap[1]
		}
		return ClassMissingThresh, "PERCENTILE_CONT arg: ground " + gs + ", agent " + as, true
	}
	return "", "", false
}

func havingClause(sql string) string {
	loc := havingPattern.FindStringIndex(sql)
	if loc == nil {
		return ""
	}
	rest := sql[loc[1]:]
	depth := 0
	end := len(rest)
scan:
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				end = i
				break scan
			}
			depth--
		case ';':
			if depth == 0 {
				end = i
				break scan
			}
		default:
			if depth == 0 && clauseEndPattern.MatchString(rest[i:]) {
				end = i
				break scan
			}
		}
	}
	return normalizeSQL(rest[:end])
}

func orderClause(sql string) string {
	m := orderByPattern.FindStringSubmatch(sql)
	if m == nil {
		return "none"
	}
	return normalizeSQL(m[1])
}

func orderDiffers(groundSQL, agentSQL string) bool {
	return orderClause(groundSQL) != orderClause(agentSQL)
}

func FormatExpected(expected any) string {
	switch v := expected.(type) {
	case nil:
		return "(no expected answer recorded)"
	case float64:
		return FormatAnswer(v)
	case string:
		return v + "\n"
	case []string:
		return FormatAnswer(v)
	case []any:
		lines := make([]string, len(v))
		for i, item := range v {
			lines[i] = fmt.Sprintf("%v", item)
		}
		return strings.Join(lines, "\n") + "\n"
	case TableAnswer:
		return FormatAnswer(v)
	case map[string]any:
		cols, _ := v["columns"].([]any)
		rows, _ := v["rows"].([]any)
		var b strings.Builder
		names := make([]string, len(cols))
		for i, c := range cols {
			names[i] = fmt.Sprintf("%v", c)
		}
		b.WriteString(strings.Join(names, ",") + "\n")
		for _, r := range rows {
			cells, _ := r.([]any)
			text := make([]string, len(cells))
			for i, c := range cells {
				text[i] = fmt.Sprintf("%v", c)
			}
			b.WriteString(strings.Join(text, ",") + "\n")
		}
		return b.String()
	default:
		return fmt.Sprintf("%v\n", v)
	}
}

func (d Diagnosis) Format() string {
	lenient, strict := "fail", "fail"
	if d.Lenient {
		lenient = "pass"
	}
	if d.Strict {
		strict = "pass"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "== %s rep=%d (%s) lenient=%s strict=%s\n", d.ID, d.Repeat, d.Difficulty, lenient, strict)
	fmt.Fprintf(&b, "question: %s\n", d.Question)
	fmt.Fprintf(&b, "expected:\n%s", indent(FormatExpected(d.Expected), "  "))
	fmt.Fprintf(&b, "ground_truth_sql: %s\n", oneLine(d.GroundSQL, 400))
	fmt.Fprintf(&b, "agent_sql: %s\n", oneLine(d.AgentSQL, 400))
	fmt.Fprintf(&b, "agent_answer:\n%s", indent(d.AgentAnswer+"\n", "  "))
	fmt.Fprintf(&b, "classification: %s: %s\n", d.Class, d.Detail)
	return b.String()
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}
