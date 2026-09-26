package bench

import (
	"strings"
	"testing"
)

func diagQuestion(id, answerType, sql string) Question {
	return Question{ID: id, Difficulty: DifficultyModerate, Text: "test question " + id, SQL: sql, AnswerType: answerType, Note: "fixture"}
}

func diagLine(id, sql, answer string, expected any) Line {
	return Line{ID: id, Difficulty: DifficultyModerate, Repeat: 1, Tier: "mid", Correct: false, Answer: answer, Expected: expected, SQL: sql, Submitted: true}
}

func TestClassifyWrongFilter(t *testing.T) {
	ground := "SELECT SUM(sale_price) FROM public.order_items WHERE status <> 'Cancelled'"
	agent := "SELECT SUM(sale_price) FROM public.order_items"
	q := diagQuestion("m01", TypeNumber, ground)
	l := diagLine("m01", agent, "100", 90.0)
	class, detail := Classify(q, l, false)
	if class != ClassWrongFilter {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongFilter, detail)
	}
	if !strings.Contains(detail, "Cancelled") {
		t.Fatalf("detail %q does not name the status value", detail)
	}
}

func TestClassifyCaseVersusWhere(t *testing.T) {
	ground := "WITH per_user AS (SELECT user_id, SUM(sale_price) AS spend FROM order_items WHERE status <> 'Cancelled' GROUP BY user_id) SELECT PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY spend) FROM per_user"
	agent := "WITH user_spend AS (SELECT user_id, SUM(CASE WHEN status <> 'Cancelled' THEN sale_price ELSE 0 END) AS gross_spend FROM order_items GROUP BY user_id) SELECT PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY gross_spend) FROM user_spend"
	q := diagQuestion("x04", TypeNumber, ground)
	l := diagLine("x04", agent, "148.08", 149.95)
	class, detail := Classify(q, l, false)
	if class != ClassWrongFilter {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongFilter, detail)
	}
	if !strings.Contains(detail, "CASE WHEN") {
		t.Fatalf("detail %q does not name the CASE form", detail)
	}
}

func TestClassifyDimensionFilter(t *testing.T) {
	ground := "SELECT COUNT(*) FROM users WHERE traffic_source = 'Facebook'"
	agent := "SELECT COUNT(*) FROM users WHERE traffic_source = 'Organic'"
	q := diagQuestion("e01", TypeNumber, ground)
	l := diagLine("e01", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassWrongFilter {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongFilter, detail)
	}
	if !strings.Contains(detail, "Facebook") || !strings.Contains(detail, "Organic") {
		t.Fatalf("detail %q does not show both sides", detail)
	}
}

func TestClassifyWrongWindow(t *testing.T) {
	ground := "SELECT COUNT(*) FROM order_items WHERE created_at >= '2024-01-01' AND created_at < '2025-01-01'"
	agent := "SELECT COUNT(*) FROM order_items WHERE created_at >= '2023-01-01' AND created_at < '2024-01-01'"
	q := diagQuestion("m07", TypeNumber, ground)
	l := diagLine("m07", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassWrongWindow {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongWindow, detail)
	}
	if !strings.Contains(detail, "2024-01-01") {
		t.Fatalf("detail %q does not show the bounds", detail)
	}
}

func TestClassifyOneSidedWindow(t *testing.T) {
	ground := "SELECT COUNT(*) FROM order_items WHERE created_at >= '2024-01-01' AND created_at < '2025-01-01'"
	agent := "SELECT COUNT(*) FROM order_items WHERE created_at < '2025-01-01'"
	q := diagQuestion("m07", TypeNumber, ground)
	l := diagLine("m07", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassWrongWindow {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongWindow, detail)
	}
	if !strings.Contains(detail, "one-sided") {
		t.Fatalf("Detail %q does not say one-sided", detail)
	}
}

func TestClassifyWrongGrainGroupBy(t *testing.T) {
	ground := "SELECT brand, COUNT(*) FROM t WHERE created_at >= '2024-01-01' GROUP BY brand"
	agent := "SELECT category, COUNT(*) FROM t WHERE created_at >= '2024-01-01' GROUP BY category"
	q := diagQuestion("m08", TypeNumber, ground)
	l := diagLine("m08", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassWrongGrain {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongGrain, detail)
	}
	if !strings.Contains(detail, "GROUP BY") {
		t.Fatalf("detail %q does not name GROUP BY", detail)
	}
}

func TestClassifyRawItemRanking(t *testing.T) {
	ground := "SELECT oi.user_id, MIN(oi.created_at) AS order_at, ROW_NUMBER() OVER (PARTITION BY oi.user_id ORDER BY MIN(oi.created_at)) FROM order_items oi GROUP BY oi.user_id"
	agent := "SELECT oi.user_id, oi.created_at, ROW_NUMBER() OVER (PARTITION BY oi.user_id ORDER BY oi.created_at) FROM order_items oi"
	q := diagQuestion("x01", TypeNumber, ground)
	l := diagLine("x01", agent, "89", 95.8)
	class, detail := Classify(q, l, false)
	if class != ClassWrongGrain {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongGrain, detail)
	}
	if !strings.Contains(detail, "MIN(created_at)") {
		t.Fatalf("detail %q does not name MIN(created_at)", detail)
	}
}

func TestClassifyDistinctOrder(t *testing.T) {
	ground := "SELECT COUNT(DISTINCT order_id) FROM order_items WHERE status <> 'Cancelled'"
	agent := "SELECT COUNT(*) FROM order_items WHERE status <> 'Cancelled'"
	q := diagQuestion("m07", TypeNumber, ground)
	l := diagLine("m07", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassWrongGrain {
		t.Fatalf("class = %q, want %q (%s)", class, ClassWrongGrain, detail)
	}
	if !strings.Contains(detail, "DISTINCT order_id") {
		t.Fatalf("detail %q does not name DISTINCT order_id", detail)
	}
}

func TestClassifyMissingLimit(t *testing.T) {
	ground := "SELECT brand FROM t GROUP BY brand ORDER BY COUNT(*) DESC LIMIT 5"
	agent := "SELECT brand FROM t GROUP BY brand ORDER BY COUNT(*) DESC"
	q := diagQuestion("m02", TypeNumber, ground)
	l := diagLine("m02", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassMissingThresh {
		t.Fatalf("class = %q, want %q (%s)", class, ClassMissingThresh, detail)
	}
	if !strings.Contains(detail, "LIMIT") {
		t.Fatalf("detail %q does not name LIMIT", detail)
	}
}

func TestClassifyStrictOnlyIsFormat(t *testing.T) {
	ground := "SELECT brand, COUNT(*) FROM t GROUP BY brand ORDER BY COUNT(*) DESC LIMIT 5"
	agent := "SELECT brand, COUNT(*) FROM t GROUP BY brand ORDER BY COUNT(*) DESC LIMIT 5"
	q := diagQuestion("m02", TypeRankedList, ground)
	l := diagLine("m02", agent, "a: 1\nb: 2", []any{"a", "b"})
	l.FormatReason = "count_appended: \"a: 1\" read as \"a\""
	class, detail := Classify(q, l, true)
	if class != ClassFormat {
		t.Fatalf("class = %q, want %q (%s)", class, ClassFormat, detail)
	}
	if !strings.Contains(detail, "count_appended") {
		t.Fatalf("detail %q does not carry the format reason", detail)
	}
}

func TestClassifyIdenticalSQLIsFormat(t *testing.T) {
	ground := "SELECT COUNT(*) FROM users"
	agent := "select  COUNT(*)  from users;"
	q := diagQuestion("e01", TypeNumber, ground)
	l := diagLine("e01", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassFormat {
		t.Fatalf("class = %q, want %q (%s)", class, ClassFormat, detail)
	}
	if !strings.Contains(detail, "matches ground truth") {
		t.Fatalf("Detail %q does not say the SQL matches", detail)
	}
}

func TestClassifyTableValuesMatchIsFormat(t *testing.T) {
	ground := "SELECT to_char(created_at, 'YYYY-MM') AS month, COUNT(DISTINCT order_id) FROM order_items WHERE status <> 'Cancelled' GROUP BY 1 ORDER BY 1"
	agent := "SELECT DATE_TRUNC('month', created_at) AS month, COUNT(DISTINCT order_id) FROM order_items WHERE status <> 'Cancelled' GROUP BY 1 ORDER BY 1"
	expected := map[string]any{
		"columns": []any{"month", "orders"},
		"rows": []any{
			[]any{"2024-01", "3417"},
			[]any{"2024-02", "3468"},
		},
	}
	q := diagQuestion("m07", TypeTable, ground)
	l := diagLine("m07", agent, "2024-01-01 00:00:00,3417\n2024-02-01 00:00:00,3468", expected)
	class, detail := Classify(q, l, false)
	if class != ClassFormat {
		t.Fatalf("class = %q, want %q (%s)", class, ClassFormat, detail)
	}
	if !strings.Contains(detail, "values match") {
		t.Fatalf("Detail %q does not say values match", detail)
	}
}

func TestClassifyAgentError(t *testing.T) {
	q := diagQuestion("e01", TypeNumber, "SELECT 1")
	l := diagLine("e01", "", "", 1.0)
	l.Error = "route: context deadline exceeded"
	l.Submitted = false
	class, _ := Classify(q, l, false)
	if class != ClassAgentError {
		t.Fatalf("class = %q, want %q", class, ClassAgentError)
	}
	l = diagLine("e01", "", "text without sql", 1.0)
	l.Submitted = true
	class, detail := Classify(q, l, false)
	if class != ClassAgentError {
		t.Fatalf("class = %q, want %q (%s)", class, ClassAgentError, detail)
	}
}

func TestClassifyOrderOnlyIsUnclassified(t *testing.T) {
	ground := "SELECT brand, COUNT(*) AS n FROM t GROUP BY brand ORDER BY n DESC, brand ASC"
	agent := "SELECT brand, COUNT(*) AS n FROM t GROUP BY brand ORDER BY n ASC"
	q := diagQuestion("m02", TypeNumber, ground)
	l := diagLine("m02", agent, "5", 10.0)
	class, detail := Classify(q, l, false)
	if class != ClassUnclassified {
		t.Fatalf("class = %q, want %q (%s)", class, ClassUnclassified, detail)
	}
	if !strings.Contains(detail, "ORDER BY") {
		t.Fatalf("Detail %q does not name ORDER BY", detail)
	}
}

func TestDiagnoseStrictFlagAndStale(t *testing.T) {
	numQ := diagQuestion("m01", TypeNumber, "SELECT SUM(sale_price) FROM order_items WHERE status <> 'Cancelled'")
	listQ := diagQuestion("m02", TypeRankedList, "SELECT brand FROM t GROUP BY brand ORDER BY COUNT(*) DESC LIMIT 5")
	staleQ := diagQuestion("m03", TypeNumber, "SELECT COUNT(*) FROM users")
	byID := map[string]Question{"m01": numQ, "m02": listQ, "m03": staleQ}
	wrong := diagLine("m01", "SELECT SUM(sale_price) FROM order_items", "100", 90.0)
	strictOnly := diagLine("m02", listQ.SQL, "a: 1\nb: 2", []any{"a", "b"})
	strictOnly.Correct = true
	stale := diagLine("m03", staleQ.SQL, "7", 7.0)
	lines := []Line{wrong, strictOnly, stale}
	out, err := Diagnose(lines, byID, false)
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 3 || out.Wrong != 1 {
		t.Fatalf("total=%d wrong=%d, want 3 and 1", out.Total, out.Wrong)
	}
	if len(out.Lines) != 1 || out.Lines[0].ID != "m01" {
		t.Fatalf("lines = %+v, want only m01", out.Lines)
	}
	if out.Lines[0].Class != ClassWrongFilter {
		t.Fatalf("m01 class = %q, want %q", out.Lines[0].Class, ClassWrongFilter)
	}
	if len(out.Stale) != 1 || out.Stale[0].ID != "m03" {
		t.Fatalf("stale = %+v, want only m03", out.Stale)
	}
	out, err = Diagnose(lines, byID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 2 || out.StrictOnly != 1 {
		t.Fatalf("strict lines = %+v, want m01 and m02", out.Lines)
	}
	if out.Lines[1].ID != "m02" || out.Lines[1].Class != ClassFormat {
		t.Fatalf("m02 = %+v, want format", out.Lines[1])
	}
}

func TestDiagnoseUnknownID(t *testing.T) {
	_, err := Diagnose([]Line{diagLine("zzz", "SELECT 1", "1", 1.0)}, map[string]Question{}, false)
	if err == nil || !strings.Contains(err.Error(), "unknown question id") {
		t.Fatalf("err = %v, want unknown question id", err)
	}
}

func TestDiagnoseFreeTextUsesRecorded(t *testing.T) {
	q := diagQuestion("h01", TypeFreeText, "SELECT 1")
	l := diagLine("h01", "SELECT 1", "some text", "some text")
	l.Correct = false
	l.JudgeVerdict = "fail"
	out, err := Diagnose([]Line{l}, map[string]Question{"h01": q}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 1 || out.Lines[0].Class != ClassFormat {
		t.Fatalf("lines = %+v, want one format diagnosis", out.Lines)
	}
}

func TestDiagnosisFormat(t *testing.T) {
	d := Diagnosis{ID: "m01", Difficulty: DifficultyModerate, Repeat: 1, Question: "q?", AnswerType: TypeNumber,
		Expected: 90.0, GroundSQL: "SELECT 1", AgentSQL: "SELECT 2", AgentAnswer: "100",
		Lenient: false, Strict: false, Class: ClassWrongFilter, Detail: "status excluded"}
	text := d.Format()
	for _, want := range []string{"m01", "q?", "90", "SELECT 1", "SELECT 2", "100", ClassWrongFilter, "status excluded"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Format() =\n%s\nmissing %q", text, want)
		}
	}
}

func TestDiagnoseStrictKeepsRecordedFailThatNowPassesLenient(t *testing.T) {
	listQ := diagQuestion("m02", TypeRankedList, "SELECT brand FROM t GROUP BY brand ORDER BY COUNT(*) DESC LIMIT 5")
	l := diagLine("m02", listQ.SQL, "a: 1\nb: 2", []any{"a", "b"})
	l.Correct = false
	byID := map[string]Question{"m02": listQ}
	out, err := Diagnose([]Line{l}, byID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 1 || out.StrictOnly != 1 || out.Lines[0].Class != ClassFormat {
		t.Fatalf("strict lines = %+v strictOnly=%d, want m02 as strict-only", out.Lines, out.StrictOnly)
	}
	if len(out.Stale) != 1 || !strings.Contains(out.Stale[0].Note, "strict-only") {
		t.Fatalf("stale = %+v, want a strict-only note", out.Stale)
	}
	out, err = Diagnose([]Line{l}, byID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Lines) != 0 || len(out.Stale) != 1 || !strings.Contains(out.Stale[0].Note, "skipped") {
		t.Fatalf("lenient mode lines = %+v stale = %+v, want skipped", out.Lines, out.Stale)
	}
}

func TestDiagnoseTableValuesMatchPerLabel(t *testing.T) {
	expected := map[string]any{
		"columns": []any{"brand", "orders"},
		"rows": []any{
			[]any{"Levi", "10"},
			[]any{"Nike", "20"},
		},
	}
	if !tableValuesMatch(expected, "Nike: 20\nLevi: 10", 0) {
		t.Error("same values under the same labels in another order must match")
	}
	if tableValuesMatch(expected, "Levi: 20\nNike: 10", 0) {
		t.Error("values swapped between labels must not match")
	}
	if tableValuesMatch(expected, "Levi: 10\nAdidas: 20", 0) {
		t.Error("an unknown label must not match")
	}
	months := map[string]any{
		"columns": []any{"month", "orders"},
		"rows": []any{
			[]any{"2024-01", "3417"},
			[]any{"2024-10", "3468"},
		},
	}
	if !tableValuesMatch(months, "2024-01-01 00:00:00,3417\n2024-10-01 00:00:00,3468", 0) {
		t.Error("timestamp labels must match month labels")
	}
	if tableValuesMatch(months, "2024-01-01 00:00:00,3468\n2024-10-01 00:00:00,3417", 0) {
		t.Error("swapped month values must not match")
	}
}

func TestDiagnoseTableValuesParsesAllCSVFields(t *testing.T) {
	expected := map[string]any{
		"columns": []any{"brand", "orders", "revenue"},
		"rows": []any{
			[]any{"Levi", "10", "250.5"},
			[]any{"Nike", "20", "400"},
		},
	}
	if !tableValuesMatch(expected, "brand,orders,revenue\nLevi,10,250.5\nNike,20,400", 0) {
		t.Error("CSV rows with two value columns must match")
	}
	if tableValuesMatch(expected, "Levi,99,250.5\nNike,20,400", 0) {
		t.Error("a wrong middle CSV field must not match")
	}
}

func TestDiagnoseWindowBoundsLiteralForms(t *testing.T) {
	plain := "SELECT COUNT(*) FROM order_items WHERE created_at >= '2024-01-01' AND created_at < '2024-02-01'"
	forms := []string{
		"SELECT COUNT(*) FROM order_items WHERE created_at >= DATE '2024-01-01' AND created_at < TIMESTAMP '2024-02-01 00:00:00'",
		"SELECT COUNT(*) FROM order_items WHERE oi.created_at >= timestamp '2024-01-01T00:00:00' AND created_at < date '2024-02-01'",
	}
	want := formatBounds(windowBounds(plain))
	if want != "created_at < '2024-02-01', created_at >= '2024-01-01'" {
		t.Fatalf("plain bounds %q", want)
	}
	for _, f := range forms {
		if got := formatBounds(windowBounds(f)); got != want {
			t.Errorf("bounds for %q = %q, want %q", f, got, want)
		}
	}
	between := "SELECT COUNT(*) FROM order_items WHERE created_at BETWEEN DATE '2024-01-01' AND '2024-01-31 23:59:59'"
	if got := formatBounds(windowBounds(between)); got != "created_at <= '2024-01-31 23:59:59', created_at >= '2024-01-01'" {
		t.Errorf("between bounds %q", got)
	}
	class, detail := Classify(diagQuestion("m01", TypeNumber, plain), diagLine("m01", "SELECT COUNT(*) FROM order_items WHERE created_at >= DATE '2024-01-01'", "1", 2.0), false)
	if class != ClassWrongWindow {
		t.Errorf("class = %q, want %q (%s)", class, ClassWrongWindow, detail)
	}
}

func TestDiagnoseHavingInsideCTE(t *testing.T) {
	sql := "WITH buyers AS (SELECT user_id FROM order_items GROUP BY user_id HAVING COUNT(*) >= 2) SELECT COUNT(*) FROM buyers b JOIN users u ON u.id = b.user_id ORDER BY 1"
	if got := havingClause(sql); got != "count(*) >= 2" {
		t.Errorf("having = %q, want %q", got, "count(*) >= 2")
	}
	if got := havingClause("SELECT a FROM t GROUP BY a HAVING SUM(x) > (SELECT 1) ORDER BY a LIMIT 3"); got != "sum(x) > (select 1)" {
		t.Errorf("having = %q", got)
	}
	if got := havingClause("SELECT a FROM t GROUP BY a HAVING COUNT(*) > 1;"); got != "count(*) > 1" {
		t.Errorf("having = %q", got)
	}
	if got := havingClause("SELECT a FROM t"); got != "" {
		t.Errorf("having = %q, want empty", got)
	}
	ground := sql
	agent := "WITH buyers AS (SELECT user_id FROM order_items GROUP BY user_id HAVING COUNT(*) >= 2) SELECT COUNT(*) FROM buyers b JOIN users u ON u.id = b.user_id WHERE u.age > 0 ORDER BY 1"
	if class, detail, ok := classifyThreshold(ground, agent); ok {
		t.Errorf("same HAVING inside the CTE reported as %s: %s", class, detail)
	}
}
