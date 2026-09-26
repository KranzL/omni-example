package bench

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	ReasonAgentError    = "agent_error"
	ReasonNoAnswer      = "no_answer"
	ReasonNoNumber      = "no_number"
	ReasonWrongNumber   = "wrong_number"
	ReasonWrongValue    = "wrong_value"
	ReasonWrongItems    = "wrong_items"
	ReasonWrongOrder    = "wrong_order"
	ReasonWrongRows     = "wrong_row_labels"
	ReasonWrongValues   = "wrong_row_values"
	ReasonUnparsedRows  = "unparsed_rows"
	ReasonJudgeRejected = "judge_rejected"
	ReasonStaleVerdict  = "stale_verdict"
	ReasonUngradable    = "ungradable"

	FormatCountAppended = "count_appended"
	FormatUnitWords     = "unit_words"
	FormatMonthFormat   = "month_format"
	FormatNumberFormat  = "number_format"
	FormatCSVRows       = "csv_rows"
	FormatAnnotation    = "annotation"
	FormatOther         = "format_other"

	maxReasonItems = 5
	maxReasonText  = 60
)

func ReasonCode(reason string) string {
	code, _, _ := strings.Cut(reason, ":")
	return strings.TrimSpace(code)
}

func Explain(q Question, l Line) (string, string) {
	if l.Correct {
		if l.CorrectStrict {
			return "", ""
		}
		return "", FormatReason(q, l.Expected, l.Answer)
	}
	return FailReason(q, l), ""
}

func ApplyReasons(q Question, l *Line) {
	l.FailReason, l.FormatReason = Explain(q, *l)
}

func FailReason(q Question, l Line) string {
	if l.Error != "" {
		return ReasonAgentError + ": " + oneLine(l.Error, 160)
	}
	if !l.Submitted || strings.TrimSpace(l.Answer) == "" {
		out := ReasonNoAnswer + ": agent did not submit an answer"
		if l.Failure != "" {
			out += " (" + oneLine(l.Failure, 80) + ")"
		}
		if t := strings.TrimSpace(l.Answer); t != "" {
			out += fmt.Sprintf(", last text %q", oneLine(t, maxReasonText))
		}
		return out
	}
	if q.AnswerType == TypeFreeText {
		return ReasonJudgeRejected + ": " + oneLine(l.JudgeVerdict, 160)
	}
	if ok, err := Grade(q, l.Expected, l.Answer); err != nil {
		return ReasonUngradable + ": " + err.Error()
	} else if ok {
		return ReasonStaleVerdict + ": the current grader passes this answer; the recorded fail predates a grader change"
	}
	switch q.AnswerType {
	case TypeNumber:
		return numberFailReason(q, l.Expected, l.Answer)
	case TypeString:
		want, _ := expectedString(l.Expected)
		return fmt.Sprintf("%s: got %q, want %q", ReasonWrongValue, oneLine(normalizeLenientString(l.Answer), maxReasonText), want)
	case TypeList:
		want, _ := expectedStringList(l.Expected)
		return listFailReason(SplitLines(l.Answer), want, false)
	case TypeRankedList:
		want, _ := expectedStringList(l.Expected)
		return listFailReason(SplitLines(l.Answer), want, true)
	case TypeTable:
		want, _ := expectedTable(l.Expected)
		return tableFailReason(want, l.Answer, q.EffectiveTolerance())
	}
	return ReasonUngradable + ": answer_type " + q.AnswerType
}

func numberFailReason(q Question, expected any, got string) string {
	want, _ := expectedNumber(expected)
	v, ok := FirstNumberLenient(got)
	if !ok {
		return fmt.Sprintf("%s: answer %q has no number, want %s", ReasonNoNumber, oneLine(got, maxReasonText), fmtNum(want))
	}
	off := "want is 0"
	if want != 0 {
		off = fmt.Sprintf("off by %.2f%%", 100*math.Abs(v-want)/math.Abs(want))
	}
	return fmt.Sprintf("%s: got %s, want %s, %s, tolerance %.2f%%", ReasonWrongNumber, fmtNum(v), fmtNum(want), off, 100*q.EffectiveTolerance())
}

func fmtNum(v float64) string {
	return strconv.FormatFloat(math.Round(v*1e4)/1e4, 'f', -1, 64)
}

func fmtCells(cells []string) string {
	out := make([]string, len(cells))
	for i, c := range cells {
		if v, err := strconv.ParseFloat(strings.TrimSpace(c), 64); err == nil {
			c = fmtNum(v)
		}
		out[i] = c
	}
	return strings.Join(out, ", ")
}

func listDiff(got, want []string) ([]string, []string) {
	normGot, normWant := lenientListItems(got, want)
	used := make([]bool, len(normWant))
	var extra []string
	for _, g := range normGot {
		hit := false
		for j, w := range normWant {
			if !used[j] && lenientItemEqual(w, g) {
				used[j] = true
				hit = true
				break
			}
		}
		if !hit {
			extra = append(extra, g)
		}
	}
	var missing []string
	for j, w := range normWant {
		if !used[j] {
			missing = append(missing, w)
		}
	}
	return missing, extra
}

func listFailReason(got, want []string, ranked bool) string {
	missing, extra := listDiff(got, want)
	if len(missing) == 0 && len(extra) == 0 {
		if !ranked || len(got) != len(want) {
			return fmt.Sprintf("%s: got %d items, want %d", ReasonWrongItems, len(got), len(want))
		}
		normGot, normWant := lenientListItems(got, want)
		for i := range normGot {
			if !lenientItemEqual(normWant[i], normGot[i]) {
				return fmt.Sprintf("%s: position %d got %q, want %q", ReasonWrongOrder, i+1, normGot[i], normWant[i])
			}
		}
		return fmt.Sprintf("%s: items match but order differs", ReasonWrongOrder)
	}
	return fmt.Sprintf("%s: %s", ReasonWrongItems, missingExtra(missing, extra, len(got), len(want)))
}

func missingExtra(missing, extra []string, got, want int) string {
	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+nameList(missing))
	}
	if len(extra) > 0 {
		parts = append(parts, "extra "+nameList(extra))
	}
	if got != want {
		parts = append(parts, fmt.Sprintf("got %d, want %d", got, want))
	}
	return strings.Join(parts, "; ")
}

func nameList(items []string) string {
	shown := items
	if len(shown) > maxReasonItems {
		shown = shown[:maxReasonItems]
	}
	quoted := make([]string, len(shown))
	for i, s := range shown {
		quoted[i] = strconv.Quote(oneLine(s, maxReasonText))
	}
	out := "[" + strings.Join(quoted, ", ")
	if n := len(items) - len(shown); n > 0 {
		out += fmt.Sprintf(" and %d more", n)
	}
	return out + "]"
}

type tableIndex struct {
	byLabel    map[string][]string
	order      []string
	pinnedYear string
	hasYear    bool
}

func indexTable(want TableAnswer) tableIndex {
	idx := tableIndex{byLabel: map[string][]string{}}
	var keys []string
	for _, row := range want.Rows {
		if len(row) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(row[0]))
		if _, dup := idx.byLabel[key]; dup {
			continue
		}
		idx.byLabel[key] = row
		idx.order = append(idx.order, key)
		keys = append(keys, row[0])
	}
	idx.pinnedYear, idx.hasYear = pinnedMonthYear(keys)
	return idx
}

func tableFailReason(want TableAnswer, got string, tol float64) string {
	lines := SplitLines(got)
	if len(want.Columns) <= 1 {
		var items []string
		for _, row := range want.Rows {
			if len(row) > 0 {
				items = append(items, row[0])
			}
		}
		return listFailReason(lines, items, false)
	}
	idx := indexTable(want)
	rows := parseLenientTableRows(want, lines, idx.byLabel, idx.pinnedYear, idx.hasYear)
	seen := map[string]bool{}
	var extra, dup, unparsed, values []string
	for _, r := range rows {
		if !r.ok {
			unparsed = append(unparsed, r.line)
			continue
		}
		label, rest := r.label, r.rest
		key, ok := resolveLenientLabel(label, idx.byLabel, idx.pinnedYear, idx.hasYear)
		if !ok {
			extra = append(extra, label)
			continue
		}
		if seen[key] {
			dup = append(dup, label)
			continue
		}
		seen[key] = true
		row := idx.byLabel[key]
		if !lenientRowValuesEqual(row[1:], rest, tol) {
			values = append(values, fmt.Sprintf("%s got %s, want %s", row[0], oneLine(rest, maxReasonText), fmtCells(row[1:])))
		}
	}
	var missing []string
	for _, key := range idx.order {
		if !seen[key] {
			missing = append(missing, idx.byLabel[key][0])
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		out := ReasonWrongRows + ": " + missingExtra(missing, extra, len(rows), len(want.Rows))
		if len(values) > 0 {
			out += fmt.Sprintf("; %d matched rows also have wrong values", len(values))
		}
		return out
	}
	if len(values) > 0 {
		shown := values
		if len(shown) > 3 {
			shown = shown[:3]
		}
		out := fmt.Sprintf("%s: %d of %d rows wrong: %s", ReasonWrongValues, len(values), len(want.Rows), strings.Join(shown, "; "))
		if len(values) > len(shown) {
			out += fmt.Sprintf("; and %d more", len(values)-len(shown))
		}
		return out
	}
	if len(dup) > 0 {
		return ReasonWrongRows + ": duplicate labels " + nameList(dup)
	}
	if len(unparsed) > 0 {
		return fmt.Sprintf("%s: %d answer lines lack label: value shape, first %q", ReasonUnparsedRows, len(unparsed), oneLine(unparsed[0], maxReasonText))
	}
	return fmt.Sprintf("%s: got %d rows, want %d", ReasonWrongRows, len(rows), len(want.Rows))
}

func FormatReason(q Question, expected any, got string) string {
	switch q.AnswerType {
	case TypeNumber:
		v, _ := FirstNumber(got)
		lv, _ := FirstNumberLenient(got)
		if v != lv {
			return fmt.Sprintf("%s: %q read as %s", FormatNumberFormat, oneLine(strings.TrimSpace(got), maxReasonText), fmtNum(lv))
		}
	case TypeString:
		if s := strings.TrimSpace(normalizeString(got)); s != normalizeLenientString(got) {
			return fmt.Sprintf("%s: answer adds %q after the value", FormatAnnotation, oneLine(strings.TrimSpace(strings.TrimPrefix(s, normalizeLenientString(got))), maxReasonText))
		}
	case TypeList, TypeRankedList:
		want, err := expectedStringList(expected)
		if err == nil {
			if r := listFormatReason(SplitLines(got), want); r != "" {
				return r
			}
		}
	case TypeTable:
		want, err := expectedTable(expected)
		if err == nil {
			if r := tableFormatReason(want, got); r != "" {
				return r
			}
		}
	}
	return FormatOther + ": strict and lenient verdicts differ for a reason not classified"
}

func listFormatReason(got, want []string) string {
	keepFull := expectedHasColon(want)
	normGot, _ := lenientListItems(got, want)
	wantSet := toSet(want)
	for i, line := range got {
		raw := strings.TrimSpace(line)
		if wantSet[strings.ToLower(raw)] {
			continue
		}
		stripped := normalizeListAnswerItem(raw, keepFull)
		if stripped != raw {
			code := FormatAnnotation
			if strings.ContainsAny(strings.TrimPrefix(raw, stripped), "0123456789") {
				code = FormatCountAppended
			}
			return fmt.Sprintf("%s: %q read as %q", code, oneLine(raw, maxReasonText), normGot[i])
		}
		if monthShortPattern.MatchString(normGot[i]) {
			return fmt.Sprintf("%s: %q read as %s", FormatMonthFormat, oneLine(raw, maxReasonText), normGot[i])
		}
	}
	return ""
}

func tableFormatReason(want TableAnswer, got string) string {
	lines := SplitLines(got)
	if len(want.Columns) <= 1 {
		var items []string
		for _, row := range want.Rows {
			if len(row) > 0 {
				items = append(items, row[0])
			}
		}
		return listFormatReason(lines, items)
	}
	idx := indexTable(want)
	rows := parseLenientTableRows(want, lines, idx.byLabel, idx.pinnedYear, idx.hasYear)
	if len(rows) > 0 && rows[0].csv {
		return fmt.Sprintf("%s: %q uses comma-separated rows, want label: value", FormatCSVRows, oneLine(rows[0].line, maxReasonText))
	}
	for _, r := range rows {
		if !r.ok {
			continue
		}
		label, rest := r.label, r.rest
		key, ok := resolveLenientLabel(label, idx.byLabel, idx.pinnedYear, idx.hasYear)
		if !ok {
			continue
		}
		row := idx.byLabel[key]
		strictLabel, _, _ := strings.Cut(strings.TrimSpace(r.line), ":")
		if strings.ToLower(strings.TrimSpace(strictLabel)) != key && monthShortPattern.MatchString(strings.TrimSpace(row[0])) {
			return fmt.Sprintf("%s: row label %q, want %s", FormatMonthFormat, oneLine(label, maxReasonText), row[0])
		}
		if strictRowValuesEqual(row[1:], rest, math.Inf(1)) {
			continue
		}
		if hasWords(rest) {
			return fmt.Sprintf("%s: row %q value %q", FormatUnitWords, row[0], oneLine(rest, maxReasonText))
		}
		return fmt.Sprintf("%s: row %q value %q", FormatNumberFormat, row[0], oneLine(rest, maxReasonText))
	}
	return ""
}

func hasWords(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r == '$' || r == '%' || r == '=' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	})
}

func CountReasons(lines []Line) (map[string]int, map[string]int) {
	var fail, format map[string]int
	for _, l := range lines {
		if l.FailReason != "" {
			if fail == nil {
				fail = map[string]int{}
			}
			fail[ReasonCode(l.FailReason)]++
		}
		if l.FormatReason != "" {
			if format == nil {
				format = map[string]int{}
			}
			format[ReasonCode(l.FormatReason)]++
		}
	}
	return fail, format
}

func FormatReasonCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, counts[k])
	}
	return strings.Join(parts, " ")
}
