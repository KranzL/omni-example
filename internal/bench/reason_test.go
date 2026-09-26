package bench

import (
	"strings"
	"testing"
)

func TestFailReason(t *testing.T) {
	num := Question{ID: "e04", AnswerType: TypeNumber}
	str := Question{ID: "h06", AnswerType: TypeString}
	list := Question{ID: "e08", AnswerType: TypeList}
	ranked := Question{ID: "m02", AnswerType: TypeRankedList}
	table := Question{ID: "m06", AnswerType: TypeTable}
	tbl := map[string]any{
		"columns": []any{"source", "revenue"},
		"rows":    []any{[]any{"Search", "100.5"}, []any{"Facebook", "40"}, []any{"Email", "10"}},
	}
	cases := []struct {
		name string
		q    Question
		l    Line
		want []string
	}{
		{"agent error", num, Line{Error: "turn 1: 400 Bad Request", Expected: 1968.0}, []string{ReasonAgentError + ": turn 1: 400 Bad Request"}},
		{"not submitted", num, Line{Submitted: false, Answer: "Let me retry", Failure: "no submit_answer call after nudge", Expected: 1968.0}, []string{ReasonNoAnswer, "no submit_answer call after nudge", `"Let me retry"`}},
		{"empty answer", num, Line{Submitted: true, Answer: " ", Expected: 1968.0}, []string{ReasonNoAnswer}},
		{"wrong number", num, Line{Submitted: true, Answer: "1900 items", Expected: 1968.0}, []string{"wrong_number: got 1900, want 1968, off by 3.46%, tolerance 1.00%"}},
		{"no number", num, Line{Submitted: true, Answer: "about two thousand", Expected: 1968.0}, []string{ReasonNoNumber, `"about two thousand"`, "want 1968"}},
		{"wrong string", str, Line{Submitted: true, Answer: "Search", Expected: "Facebook"}, []string{`wrong_value: got "Search", want "Facebook"`}},
		{"list items", list, Line{Submitted: true, Answer: "Complete\nShipped\nPending", Expected: []any{"Complete", "Returned", "Cancelled"}}, []string{`wrong_items: missing ["Returned", "Cancelled"]; extra ["Shipped", "Pending"]`}},
		{"list count", list, Line{Submitted: true, Answer: "Complete, Returned", Expected: []any{"Complete", "Returned"}}, []string{`missing ["Complete", "Returned"]`, `extra ["Complete, Returned"]`, "got 1, want 2"}},
		{"ranked order", ranked, Line{Submitted: true, Answer: "Dockers\nLevi's", Expected: []any{"Levi's", "Dockers"}}, []string{`wrong_order: position 1 got "Dockers", want "Levi's"`}},
		{"table labels", table, Line{Submitted: true, Answer: "Search: 100.5\nFacebook: 40\nDisplay: 3", Expected: tbl}, []string{`wrong_row_labels: missing ["Email"]; extra ["Display"]`}},
		{"table values", table, Line{Submitted: true, Answer: "Search: 100.5\nFacebook: 44\nEmail: 10", Expected: tbl}, []string{"wrong_row_values: 1 of 3 rows wrong: Facebook got 44, want 40"}},
		{"table shape", table, Line{Submitted: true, Answer: "Search 100.5\nFacebook 40\nEmail 10", Expected: tbl}, []string{ReasonWrongRows, `missing ["Search", "Facebook", "Email"]`}},
		{"stale verdict", num, Line{Submitted: true, Answer: "1968", Expected: 1968.0}, []string{ReasonStaleVerdict}},
	}
	for _, c := range cases {
		got := FailReason(c.q, c.l)
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: reason %q lacks %q", c.name, got, w)
			}
		}
	}
}

func TestFormatReason(t *testing.T) {
	months := map[string]any{
		"columns": []any{"month", "orders"},
		"rows":    []any{[]any{"2024-01", "3417"}, []any{"2024-02", "3468"}},
	}
	cohort := map[string]any{
		"columns": []any{"month", "users", "revenue"},
		"rows":    []any{[]any{"2024-01", "2300", "167962.75"}},
	}
	cases := []struct {
		name string
		q    Question
		exp  any
		ans  string
		want string
	}{
		{"count appended", Question{AnswerType: TypeRankedList}, []any{"Levi's", "Dockers"}, "Levi's: 3013\nDockers: 1289", FormatCountAppended + `: "Levi's: 3013" read as "Levi's"`},
		{"parenthetical count", Question{AnswerType: TypeList}, []any{"Jeans"}, "Jeans (123)", FormatCountAppended},
		{"annotation", Question{AnswerType: TypeList}, []any{"Jeans"}, "Jeans (denim)", FormatAnnotation},
		{"list month", Question{AnswerType: TypeList}, []any{"2024-01", "2024-02"}, "January 2024\nFebruary 2024", FormatMonthFormat + `: "January 2024" read as 2024-01`},
		{"table month", Question{AnswerType: TypeTable}, months, "2024-01-01 00:00:00: 3417\n2024-02-01 00:00:00: 3468", FormatMonthFormat + `: row label "2024-01-01 00:00:00", want 2024-01`},
		{"unit words", Question{AnswerType: TypeTable}, cohort, "2024-01: 2300 users, $167962.75 net revenue", FormatUnitWords + `: row "2024-01"`},
		{"string annotation", Question{AnswerType: TypeString}, "Facebook", "Facebook (54.15 per user)", FormatAnnotation + `: answer adds "(54.15 per user)"`},
	}
	for _, c := range cases {
		q := c.q
		q.ID = "t"
		lenient, err := Grade(q, c.exp, c.ans)
		if err != nil || !lenient {
			t.Fatalf("%s: lenient %v %v, fixture must pass lenient", c.name, lenient, err)
		}
		if strict, _ := GradeStrict(q, c.exp, c.ans); strict {
			t.Fatalf("%s: fixture passes strict", c.name)
		}
		got := FormatReason(q, c.exp, c.ans)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: format reason %q lacks %q", c.name, got, c.want)
		}
	}
}

func TestExplainAndCounts(t *testing.T) {
	q := Question{ID: "m02", AnswerType: TypeRankedList}
	exp := []any{"Levi's", "Dockers"}
	lines := []Line{
		{ID: "m02", Correct: true, CorrectStrict: true, Submitted: true, Answer: "Levi's\nDockers", Expected: exp},
		{ID: "m02", Correct: true, CorrectStrict: false, Submitted: true, Answer: "Levi's: 1\nDockers: 2", Expected: exp},
		{ID: "m02", Correct: false, Submitted: true, Answer: "Dockers\nLevi's", Expected: exp},
		{ID: "m02", Correct: false, Error: "boom", Expected: exp},
	}
	for i := range lines {
		ApplyReasons(q, &lines[i])
	}
	if lines[0].FailReason != "" || lines[0].FormatReason != "" {
		t.Errorf("passing line got reasons %+v", lines[0])
	}
	if lines[1].FailReason != "" || ReasonCode(lines[1].FormatReason) != FormatCountAppended {
		t.Errorf("strict-only line: %q %q", lines[1].FailReason, lines[1].FormatReason)
	}
	if ReasonCode(lines[2].FailReason) != ReasonWrongOrder || lines[2].FormatReason != "" {
		t.Errorf("wrong order line: %q %q", lines[2].FailReason, lines[2].FormatReason)
	}
	fail, format := CountReasons(lines)
	if fail[ReasonWrongOrder] != 1 || fail[ReasonAgentError] != 1 || len(fail) != 2 || format[FormatCountAppended] != 1 || len(format) != 1 {
		t.Errorf("counts fail=%v format=%v", fail, format)
	}
	s := Summarize("c", "t", "p", 1, lines)
	if s.FailReasons[ReasonAgentError] != 1 || s.FormatReasons[FormatCountAppended] != 1 {
		t.Errorf("summary counts fail=%v format=%v", s.FailReasons, s.FormatReasons)
	}
	if got := FormatReasonCounts(fail); got != "agent_error=1 wrong_order=1" {
		t.Errorf("FormatReasonCounts = %q", got)
	}
}
