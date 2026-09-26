package bench

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/llm"
)

var numberPattern = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

var lenientNumberPattern = regexp.MustCompile(`[-+]?\$?(?:\d{1,3}(?:,\d{3})+\b(?:\.\d*)?|\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)

var monthShortPattern = regexp.MustCompile(`^\d{4}-\d{2}$`)

var monthDatePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var monthDateTimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

var monthNameToNum = map[string]string{
	"january": "01", "jan": "01",
	"february": "02", "feb": "02",
	"march": "03", "mar": "03",
	"april": "04", "apr": "04",
	"may":  "05",
	"june": "06", "jun": "06",
	"july": "07", "jul": "07",
	"august": "08", "aug": "08",
	"september": "09", "sep": "09", "sept": "09",
	"october": "10", "oct": "10",
	"november": "11", "nov": "11",
	"december": "12", "dec": "12",
}

func pinnedMonthYear(keys []string) (string, bool) {
	if len(keys) == 0 {
		return "", false
	}
	year := ""
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if !monthShortPattern.MatchString(k) {
			return "", false
		}
		if year == "" {
			year = k[:4]
		} else if k[:4] != year {
			return "", false
		}
	}
	return year, true
}

func monthNameKey(label, year string) string {
	s := strings.ToLower(strings.TrimSpace(label))
	s = strings.TrimSpace(strings.TrimSuffix(s, "."))
	if mm, ok := monthNameToNum[s]; ok {
		return year + "-" + mm
	}
	fields := strings.Fields(s)
	if len(fields) == 2 {
		a := strings.TrimSuffix(fields[0], ".")
		b := strings.TrimSuffix(fields[1], ".")
		if a == year {
			if mm, ok := monthNameToNum[b]; ok {
				return year + "-" + mm
			}
		}
		if b == year {
			if mm, ok := monthNameToNum[a]; ok {
				return year + "-" + mm
			}
		}
	}
	return ""
}

func WithinTolerance(got, want, tol float64) bool {
	if math.IsNaN(got) || math.IsNaN(want) {
		return false
	}
	if want == 0 {
		return got == 0
	}
	return math.Abs(got-want)/math.Abs(want) <= tol
}

func FirstNumber(s string) (float64, bool) {
	m := numberPattern.FindString(s)
	if m == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func FirstNumberLenient(s string) (float64, bool) {
	m := lenientNumberPattern.FindString(s)
	if m == "" {
		return 0, false
	}
	m = strings.NewReplacer("$", "", ",", "").Replace(m)
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func normalizeString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			s = strings.TrimSpace(s[1 : len(s)-1])
		}
	}
	return s
}

func SplitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func Grade(q Question, expected any, got string) (bool, error) {
	switch q.AnswerType {
	case TypeNumber:
		want, err := expectedNumber(expected)
		if err != nil {
			return false, err
		}
		v, ok := FirstNumberLenient(got)
		if !ok {
			return false, nil
		}
		return WithinTolerance(v, want, q.EffectiveTolerance()), nil
	case TypeString:
		want, err := expectedString(expected)
		if err != nil {
			return false, err
		}
		return lenientStringEqual(got, want), nil
	case TypeList:
		want, err := expectedStringList(expected)
		if err != nil {
			return false, err
		}
		return equalAsSetLenient(SplitLines(got), want), nil
	case TypeRankedList:
		want, err := expectedStringList(expected)
		if err != nil {
			return false, err
		}
		return equalOrderedLenient(SplitLines(got), want), nil
	case TypeTable:
		want, err := expectedTable(expected)
		if err != nil {
			return false, err
		}
		return gradeTableLenient(want, got, q.EffectiveTolerance()), nil
	case TypeFreeText:
		return false, fmt.Errorf("question %s is free_text and needs JudgeFreeText", q.ID)
	default:
		return false, fmt.Errorf("bad answer_type %q", q.AnswerType)
	}
}

func GradeStrict(q Question, expected any, got string) (bool, error) {
	switch q.AnswerType {
	case TypeNumber:
		want, err := expectedNumber(expected)
		if err != nil {
			return false, err
		}
		v, ok := FirstNumber(got)
		if !ok {
			return false, nil
		}
		return WithinTolerance(v, want, q.EffectiveTolerance()), nil
	case TypeString:
		want, err := expectedString(expected)
		if err != nil {
			return false, err
		}
		return strings.EqualFold(normalizeString(got), normalizeString(want)), nil
	case TypeList:
		want, err := expectedStringList(expected)
		if err != nil {
			return false, err
		}
		return equalAsSet(SplitLines(got), want), nil
	case TypeRankedList:
		want, err := expectedStringList(expected)
		if err != nil {
			return false, err
		}
		return equalOrdered(SplitLines(got), want), nil
	case TypeTable:
		want, err := expectedTable(expected)
		if err != nil {
			return false, err
		}
		return gradeTable(want, got, q.EffectiveTolerance()), nil
	case TypeFreeText:
		return false, fmt.Errorf("question %s is free_text and needs JudgeFreeText", q.ID)
	default:
		return false, fmt.Errorf("bad answer_type %q", q.AnswerType)
	}
}

func equalAsSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	counts := make(map[string]int, len(want))
	for _, w := range want {
		counts[strings.ToLower(strings.TrimSpace(w))]++
	}
	for _, g := range got {
		k := strings.ToLower(strings.TrimSpace(g))
		if counts[k] == 0 {
			return false
		}
		counts[k]--
	}
	return true
}

func toSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[strings.ToLower(strings.TrimSpace(item))] = true
	}
	return out
}

func equalOrdered(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !strings.EqualFold(strings.TrimSpace(got[i]), strings.TrimSpace(want[i])) {
			return false
		}
	}
	return true
}

func gradeTable(want TableAnswer, got string, tol float64) bool {
	lines := SplitLines(got)
	if len(want.Columns) <= 1 {
		var wantItems []string
		for _, row := range want.Rows {
			if len(row) > 0 {
				wantItems = append(wantItems, row[0])
			}
		}
		return equalAsSet(lines, wantItems)
	}
	if len(lines) != len(want.Rows) {
		return false
	}
	wantByLabel := make(map[string][]string, len(want.Rows))
	for _, row := range want.Rows {
		if len(row) == 0 {
			return false
		}
		key := strings.ToLower(strings.TrimSpace(row[0]))
		if _, dup := wantByLabel[key]; dup {
			return false
		}
		wantByLabel[key] = row
	}
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		label, rest, ok := strings.Cut(line, ":")
		if !ok {
			return false
		}
		label = strings.TrimSpace(label)
		rest = strings.TrimSpace(rest)
		if label == "" || rest == "" {
			return false
		}
		key := strings.ToLower(label)
		if seen[key] {
			return false
		}
		seen[key] = true
		wantRow, ok := wantByLabel[key]
		if !ok {
			return false
		}
		fields := splitTableValues(rest)
		if len(fields) != len(wantRow)-1 {
			return false
		}
		for i, field := range fields {
			wantCell := wantRow[i+1]
			wantNum, wantErr := strconv.ParseFloat(strings.TrimSpace(wantCell), 64)
			gotNum, gotErr := strconv.ParseFloat(field, 64)
			if wantErr == nil && gotErr == nil {
				if !WithinTolerance(gotNum, wantNum, tol) {
					return false
				}
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(field), strings.TrimSpace(wantCell)) {
				return false
			}
		}
	}
	return true
}

func splitTableValues(s string) []string {
	raw := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	out := raw[:0]
	for _, f := range raw {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func stripParenthetical(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, ")") {
		return s
	}
	idx := strings.LastIndex(s, "(")
	if idx < 0 {
		return s
	}
	return strings.TrimSpace(s[:idx])
}

func normalizeLenientString(s string) string {
	return strings.TrimSpace(stripParenthetical(normalizeString(s)))
}

func lenientStringEqual(got, want string) bool {
	g := normalizeString(got)
	w := normalizeString(want)
	if strings.EqualFold(g, w) {
		return true
	}
	if stripParenthetical(w) != w {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(stripParenthetical(g)), w)
}

func expectedHasColon(want []string) bool {
	for _, w := range want {
		if strings.Contains(w, ":") {
			return true
		}
	}
	return false
}

func normalizeListAnswerItem(line string, keepFull bool) string {
	s := strings.TrimSpace(line)
	s = strings.TrimSpace(stripParenthetical(s))
	if !keepFull {
		if monthDatePattern.MatchString(s) || monthDateTimePattern.MatchString(s) {
			return s
		}
		if len(s) >= 19 && monthDateTimePattern.MatchString(s[:19]) {
			after := s[19:]
			if idx := strings.Index(after, ":"); idx >= 0 {
				return strings.TrimSpace(s[:19])
			}
			return s
		}
		if label, _, ok := strings.Cut(s, ":"); ok {
			s = label
		}
	}
	s = stripParenthetical(s)
	return strings.TrimSpace(s)
}

func normalizeListWantItem(want string) string {
	return strings.TrimSpace(stripParenthetical(want))
}

func monthPrefixMatch(want, got string) bool {
	if !monthShortPattern.MatchString(want) {
		return false
	}
	if !strings.HasPrefix(got, want) {
		return false
	}
	return monthDatePattern.MatchString(got) || monthDateTimePattern.MatchString(got)
}

func lenientItemEqual(want, got string) bool {
	if strings.EqualFold(want, got) {
		return true
	}
	return monthPrefixMatch(want, got)
}

func lenientListItems(got, want []string) ([]string, []string) {
	keepFull := expectedHasColon(want)
	normWant := make([]string, len(want))
	for i, w := range want {
		normWant[i] = normalizeListWantItem(w)
	}
	year, hasYear := pinnedMonthYear(normWant)
	normGot := make([]string, len(got))
	for i, line := range got {
		normGot[i] = normalizeListAnswerItem(line, keepFull)
		if hasYear {
			if mk := monthNameKey(normGot[i], year); mk != "" {
				normGot[i] = mk
			}
		}
	}
	return normGot, normWant
}

func equalAsSetLenient(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	normGot, normWant := lenientListItems(got, want)
	used := make([]bool, len(normWant))
	return matchLenientSet(0, normGot, normWant, used)
}

func matchLenientSet(i int, got, want []string, used []bool) bool {
	if i >= len(got) {
		return true
	}
	for j := range want {
		if used[j] {
			continue
		}
		if !lenientItemEqual(want[j], got[i]) {
			continue
		}
		used[j] = true
		if matchLenientSet(i+1, got, want, used) {
			return true
		}
		used[j] = false
	}
	return false
}

func equalOrderedLenient(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	normGot, normWant := lenientListItems(got, want)
	for i := range normGot {
		if !lenientItemEqual(normWant[i], normGot[i]) {
			return false
		}
	}
	return true
}

func gradeTableLenient(want TableAnswer, got string, tol float64) bool {
	lines := SplitLines(got)
	if len(want.Columns) <= 1 {
		var wantItems []string
		for _, row := range want.Rows {
			if len(row) > 0 {
				wantItems = append(wantItems, row[0])
			}
		}
		return equalAsSetLenient(lines, wantItems)
	}
	wantByLabel := make(map[string][]string, len(want.Rows))
	wantKeys := make([]string, 0, len(want.Rows))
	for _, row := range want.Rows {
		if len(row) == 0 {
			return false
		}
		key := strings.ToLower(strings.TrimSpace(row[0]))
		if _, dup := wantByLabel[key]; dup {
			return false
		}
		wantByLabel[key] = row
		wantKeys = append(wantKeys, row[0])
	}
	pinnedYear, hasPinnedYear := pinnedMonthYear(wantKeys)
	rows := parseLenientTableRows(want, lines, wantByLabel, pinnedYear, hasPinnedYear)
	if len(rows) != len(want.Rows) {
		return false
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if !row.ok {
			return false
		}
		label, rest := row.label, row.rest
		key, ok := resolveLenientLabel(label, wantByLabel, pinnedYear, hasPinnedYear)
		if !ok {
			return false
		}
		wantRow := wantByLabel[key]
		if seen[key] {
			return false
		}
		seen[key] = true
		if !lenientRowValuesEqual(wantRow[1:], rest, tol) {
			return false
		}
	}
	return true
}

func resolveLenientLabel(label string, wantByLabel map[string][]string, pinnedYear string, hasPinnedYear bool) (string, bool) {
	key := strings.ToLower(label)
	if _, ok := wantByLabel[key]; ok {
		return key, true
	}
	if prefix := lenientMonthPrefix(label); prefix != "" {
		pkey := strings.ToLower(prefix)
		row, ok := wantByLabel[pkey]
		if !ok || !monthShortPattern.MatchString(strings.TrimSpace(row[0])) {
			return "", false
		}
		return pkey, true
	}
	if !hasPinnedYear {
		return "", false
	}
	mk := strings.ToLower(monthNameKey(label, pinnedYear))
	if _, ok := wantByLabel[mk]; mk == "" || !ok {
		return "", false
	}
	return mk, true
}

type lenientTableRow struct {
	line  string
	label string
	rest  string
	csv   bool
	ok    bool
}

func parseLenientTableRows(want TableAnswer, lines []string, wantByLabel map[string][]string, pinnedYear string, hasPinnedYear bool) []lenientTableRow {
	rows := make([]lenientTableRow, 0, len(lines))
	for i, line := range lines {
		row := lenientTableRow{line: line}
		if label, rest, ok := cutLenientTableLine(line); ok {
			row.label, row.rest, row.ok = label, rest, true
			rows = append(rows, row)
			continue
		}
		fields, ok := splitCSVTableLine(line)
		if !ok {
			rows = append(rows, row)
			continue
		}
		if i == 0 && isCSVHeader(fields, want.Columns, wantByLabel, pinnedYear, hasPinnedYear) {
			continue
		}
		row.label = fields[0]
		row.rest = strings.Join(fields[1:], ", ")
		row.csv = true
		row.ok = row.label != "" && strings.TrimSpace(row.rest) != ""
		rows = append(rows, row)
	}
	return rows
}

func splitCSVTableLine(line string) ([]string, bool) {
	if !strings.Contains(line, ",") {
		return nil, false
	}
	r := csv.NewReader(strings.NewReader(line))
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	fields, err := r.Read()
	if err != nil {
		fields = strings.Split(line, ",")
	}
	if len(fields) < 2 {
		return nil, false
	}
	out := make([]string, len(fields))
	for i, f := range fields {
		f = strings.TrimSpace(f)
		if i > 0 {
			f = strings.ReplaceAll(f, ",", "")
		}
		out[i] = f
	}
	return out, true
}

func isCSVHeader(fields, columns []string, wantByLabel map[string][]string, pinnedYear string, hasPinnedYear bool) bool {
	if len(fields) == len(columns) {
		match := true
		for i, f := range fields {
			if !strings.EqualFold(f, strings.TrimSpace(columns[i])) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	if _, ok := resolveLenientLabel(fields[0], wantByLabel, pinnedYear, hasPinnedYear); ok {
		return false
	}
	for _, f := range fields[1:] {
		f = strings.Trim(strings.ReplaceAll(f, ",", ""), "$% ")
		if _, err := strconv.ParseFloat(f, 64); err == nil {
			return false
		}
	}
	return true
}

func cutLenientTableLine(line string) (string, string, bool) {
	s := strings.TrimSpace(line)
	if len(s) >= 19 && monthDateTimePattern.MatchString(s[:19]) {
		after := s[19:]
		idx := strings.Index(after, ":")
		if idx < 0 {
			return "", "", false
		}
		label := strings.TrimSpace(s[:19])
		rest := strings.TrimSpace(after[idx+1:])
		if label == "" || rest == "" {
			return "", "", false
		}
		return label, rest, true
	}
	label, rest, ok := strings.Cut(s, ":")
	if !ok {
		return "", "", false
	}
	label = strings.TrimSpace(label)
	rest = strings.TrimSpace(rest)
	if label == "" || rest == "" {
		return "", "", false
	}
	return label, rest, true
}

func lenientMonthPrefix(label string) string {
	label = strings.TrimSpace(label)
	if monthDatePattern.MatchString(label) || monthDateTimePattern.MatchString(label) {
		if len(label) >= 7 {
			return label[:7]
		}
	}
	return ""
}

func lenientRowValuesEqual(wantCells []string, rest string, tol float64) bool {
	wantNums := make([]float64, 0, len(wantCells))
	for _, cell := range wantCells {
		v, err := strconv.ParseFloat(strings.TrimSpace(cell), 64)
		if err != nil {
			return strictRowValuesEqual(wantCells, rest, tol)
		}
		wantNums = append(wantNums, v)
	}
	found := numberPattern.FindAllString(rest, -1)
	if len(found) != len(wantNums) {
		return false
	}
	for i, m := range found {
		v, err := strconv.ParseFloat(m, 64)
		if err != nil {
			return false
		}
		if !WithinTolerance(v, wantNums[i], tol) {
			return false
		}
	}
	return true
}

func strictRowValuesEqual(wantCells []string, rest string, tol float64) bool {
	fields := splitTableValues(rest)
	if len(fields) != len(wantCells) {
		return false
	}
	for i, field := range fields {
		wantCell := wantCells[i]
		wantNum, wantErr := strconv.ParseFloat(strings.TrimSpace(wantCell), 64)
		gotNum, gotErr := strconv.ParseFloat(field, 64)
		if wantErr == nil && gotErr == nil {
			if !WithinTolerance(gotNum, wantNum, tol) {
				return false
			}
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(field), strings.TrimSpace(wantCell)) {
			return false
		}
	}
	return true
}

func expectedNumber(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case json.Number:
		return v.Float64()
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, fmt.Errorf("number expected value %q is not numeric", v)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("number expected value has type %T", raw)
	}
}

func expectedString(raw any) (string, error) {
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("string expected value has type %T", raw)
	}
	return s, nil
}

func expectedStringList(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("string list item %d has type %T", i, item)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("string list has type %T", raw)
	}
}

func expectedTable(raw any) (TableAnswer, error) {
	switch v := raw.(type) {
	case TableAnswer:
		return v, nil
	case map[string]any:
		return tableFromMap(v)
	default:
		return TableAnswer{}, fmt.Errorf("table expected value has type %T", raw)
	}
}

func tableFromMap(m map[string]any) (TableAnswer, error) {
	var out TableAnswer
	cols, err := expectedStringList(m["columns"])
	if err != nil {
		return out, fmt.Errorf("table columns: %w", err)
	}
	rowsRaw, ok := m["rows"].([]any)
	if !ok {
		return out, fmt.Errorf("table rows has type %T", m["rows"])
	}
	rows := make([][]string, len(rowsRaw))
	for i, r := range rowsRaw {
		row, err := expectedStringList(r)
		if err != nil {
			return out, fmt.Errorf("table row %d: %w", i, err)
		}
		rows[i] = row
	}
	out.Columns = cols
	out.Rows = rows
	return out, nil
}

func NormalizeExpected(q Question, raw any) (any, error) {
	switch q.AnswerType {
	case TypeNumber:
		return expectedNumber(raw)
	case TypeString, TypeFreeText:
		return expectedString(raw)
	case TypeList, TypeRankedList:
		return expectedStringList(raw)
	case TypeTable:
		return expectedTable(raw)
	default:
		return nil, fmt.Errorf("bad answer_type %q", q.AnswerType)
	}
}

func LoadSnapshot(path string) ([]SnapshotEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entries []SnapshotEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func ExpectedByID(entries []SnapshotEntry, qs []Question) (map[string]any, error) {
	byID := make(map[string]any, len(entries))
	for _, e := range entries {
		byID[e.ID] = e.Answer
	}
	out := make(map[string]any, len(qs))
	for _, q := range qs {
		raw, ok := byID[q.ID]
		if !ok {
			return nil, fmt.Errorf("missing snapshot answer for %s", q.ID)
		}
		v, err := NormalizeExpected(q, raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", q.ID, err)
		}
		out[q.ID] = v
	}
	return out, nil
}

type JudgeCaller interface {
	Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error)
}

type JudgeResult struct {
	Correct bool           `json:"correct"`
	Verdict string         `json:"verdict"`
	CostUSD float64        `json:"cost_usd"`
	Usage   llm.Usage      `json:"usage"`
	Model   string         `json:"model"`
	Record  llm.CallRecord `json:"record"`
}

const judgePromptTemplate = `You grade one data question. Reply with CORRECT or INCORRECT on the first line, then one short sentence of explanation.

Question: %s
Ground truth: %s
Submitted answer: %s`

func JudgeFreeText(ctx context.Context, caller JudgeCaller, q Question, expected any, got string) (JudgeResult, error) {
	want, err := expectedString(expected)
	if err != nil {
		return JudgeResult{}, err
	}
	prompt := fmt.Sprintf(judgePromptTemplate, q.Text, want, strings.TrimSpace(got))
	var trace llm.Trace
	msg, rec, err := caller.Call(ctx, llm.Request{
		Tier:     llm.TierCheap,
		Purpose:  "bench-judge-" + q.ID,
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	}, &trace)
	out := JudgeResult{CostUSD: rec.CostUSD, Usage: rec.Usage, Model: rec.Model, Record: rec}
	if err != nil {
		return out, err
	}
	var b strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	out.Verdict = strings.TrimSpace(b.String())
	out.Correct, err = parseJudgeVerdict(out.Verdict)
	return out, err
}

func parseJudgeVerdict(verdict string) (bool, error) {
	first, _, _ := strings.Cut(strings.TrimSpace(verdict), "\n")
	word := ""
	if fields := strings.Fields(first); len(fields) > 0 {
		word = strings.TrimFunc(fields[0], func(r rune) bool {
			return r != '?' && (unicode.IsPunct(r) || unicode.IsSymbol(r))
		})
	}
	switch strings.ToUpper(word) {
	case "CORRECT":
		return true, nil
	case "INCORRECT":
		return false, nil
	}
	return false, fmt.Errorf("judge verdict does not start with CORRECT or INCORRECT: %q", oneLine(verdict, 80))
}
