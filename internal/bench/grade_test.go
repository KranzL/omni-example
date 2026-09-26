package bench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/llm"
)

func tolPtr(v float64) *float64 {
	return &v
}

func TestGradeNumber(t *testing.T) {
	q := Question{ID: "e03", AnswerType: TypeNumber, Tolerance: tolPtr(0.01)}
	cases := []struct {
		name     string
		expected any
		got      string
		want     bool
	}{
		{"exact", 10.0, "10", true},
		{"within tolerance", 100.0, "100.5", true},
		{"outside tolerance", 100.0, "102", false},
		{"first number wins", 42.0, "about 42 out of 100", true},
		{"first number wrong", 100.0, "about 42 out of 100", false},
		{"decimals", 47.17632051733279, "47.18", true},
		{"scientific", 1838928.50, "1.8389285062675476e+06", true},
		{"no number", 10.0, "ten", false},
		{"zero match", 0.0, "0", true},
		{"zero mismatch", 0.0, "0.5", false},
	}
	for _, c := range cases {
		got, err := Grade(q, c.expected, c.got)
		if err != nil {
			t.Errorf("%s: Grade = %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Grade = %v, want %v", c.name, got, c.want)
		}
	}
	strict := Question{ID: "e03", AnswerType: TypeNumber, Tolerance: tolPtr(0)}
	if ok, _ := Grade(strict, 100.0, "100"); !ok {
		t.Error("zero tolerance exact: got false, want true")
	}
	if ok, _ := Grade(strict, 100.0, "100.0001"); ok {
		t.Error("zero tolerance near miss: got true, want false")
	}
	if _, err := Grade(q, "abc", "10"); err == nil {
		t.Error("non-numeric expected: want error")
	}
}

func TestGradeString(t *testing.T) {
	q := Question{ID: "e01", AnswerType: TypeString}
	cases := []struct {
		name     string
		expected string
		got      string
		want     bool
	}{
		{"exact", "jgarcia@gmail.com", "jgarcia@gmail.com", true},
		{"case differs", "Facebook", "facebook", true},
		{"whitespace", "2024-11", "  2024-11\n", true},
		{"mismatch", "Facebook", "Search", false},
		{"empty", "Facebook", "", false},
		{"double quotes", "Dockers Mens Pant", "\"Dockers Mens Pant\"", true},
		{"single quotes", "Dockers Mens Pant", "'Dockers Mens Pant'", true},
	}
	for _, c := range cases {
		got, err := Grade(q, c.expected, c.got)
		if err != nil {
			t.Errorf("%s: Grade = %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Grade = %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := Grade(q, 42.0, "42"); err == nil {
		t.Error("numeric expected for string: want error")
	}
}

func TestGradeList(t *testing.T) {
	q := Question{ID: "e08", AnswerType: TypeList}
	want := []string{"Cancelled", "Complete", "Returned"}
	cases := []struct {
		name string
		got  string
		want bool
	}{
		{"same order", "Cancelled\nComplete\nReturned", true},
		{"shuffled", "Returned\nCancelled\nComplete", true},
		{"case differs", "cancelled\ncomplete\nreturned", true},
		{"blank lines ignored", "Cancelled\n\nComplete\nReturned\n", true},
		{"missing item", "Cancelled\nComplete", false},
		{"extra item", "Cancelled\nComplete\nReturned\nPending", false},
		{"wrong item", "Cancelled\nComplete\nPending", false},
		{"single line csv is one item", "Cancelled, Complete, Returned", false},
	}
	for _, c := range cases {
		got, err := Grade(q, want, c.got)
		if err != nil {
			t.Errorf("%s: Grade = %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Grade = %v, want %v", c.name, got, c.want)
		}
	}
	raw := []any{"a", "b"}
	if _, err := Grade(q, raw, "a\nb"); err != nil {
		t.Errorf("any slice expected: Grade = %v", err)
	}
	if _, err := Grade(q, "abc", "a"); err == nil {
		t.Error("string expected for list: want error")
	}
}

func TestGradeRankedList(t *testing.T) {
	q := Question{ID: "m02", AnswerType: TypeRankedList}
	want := []string{"Levi's", "Dockers", "Allegra K", "Columbia", "Ray-Ban"}
	if ok, _ := Grade(q, want, "Levi's\nDockers\nAllegra K\nColumbia\nRay-Ban"); !ok {
		t.Error("exact order: got false, want true")
	}
	if ok, _ := Grade(q, want, "levi's\ndockers\nallegra k\ncolumbia\nray-ban"); !ok {
		t.Error("case-insensitive order: got false, want true")
	}
	if ok, _ := Grade(q, want, "Dockers\nLevi's\nAllegra K\nColumbia\nRay-Ban"); ok {
		t.Error("swapped order: got true, want false")
	}
	if ok, _ := Grade(q, want, "Levi's\nDockers\nAllegra K\nColumbia"); ok {
		t.Error("short list: got true, want false")
	}
	if ok, _ := Grade(q, want, "Levi's\nDockers\nAllegra K\nColumbia\nRay-Ban\nExtra"); ok {
		t.Error("long list: got true, want false")
	}
}

func TestGradeTableSingleValue(t *testing.T) {
	q := Question{ID: "m01", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	want := TableAnswer{
		Columns: []string{"category", "revenue"},
		Rows: [][]string{
			{"Jeans", "382665.62"},
			{"Accessories", "232113.37"},
		},
	}
	cases := []struct {
		name string
		got  string
		want bool
	}{
		{"exact labels", "Jeans: 382665.62\nAccessories: 232113.37", true},
		{"shuffled rows", "Accessories: 232113.37\nJeans: 382665.62", true},
		{"within tolerance", "Jeans: 382600\nAccessories: 232000", true},
		{"outside tolerance", "Jeans: 300000\nAccessories: 232113.37", false},
		{"label case differs", "jeans: 382665.62\naccessories: 232113.37", true},
		{"missing row", "Jeans: 382665.62", false},
		{"extra row", "Jeans: 382665.62\nAccessories: 232113.37\nShorts: 100", false},
		{"wrong label", "Jeans: 382665.62\nShorts: 232113.37", false},
		{"missing colon", "Jeans 382665.62\nAccessories: 232113.37", false},
		{"duplicate label", "Jeans: 382665.62\nJeans: 232113.37", false},
		{"empty value", "Jeans:\nAccessories: 232113.37", false},
	}
	for _, c := range cases {
		got, err := Grade(q, want, c.got)
		if err != nil {
			t.Errorf("%s: Grade = %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Grade = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestGradeTableMultiColumn(t *testing.T) {
	q := Question{ID: "h03", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	want := TableAnswer{
		Columns: []string{"distribution_center", "revenue", "cost", "margin"},
		Rows: [][]string{
			{"Memphis TN", "245822.83", "117428.82", "128394.01"},
			{"Mobile AL", "129937.96", "63242.15", "66695.81"},
		},
	}
	if ok, _ := Grade(q, want, "Memphis TN: 245822.83, 117428.82, 128394.01\nMobile AL: 129937.96, 63242.15, 66695.81"); !ok {
		t.Error("comma separated: got false, want true")
	}
	if ok, _ := Grade(q, want, "Memphis TN: 245822.83 117428.82 128394.01\nMobile AL: 129937.96 63242.15 66695.81"); !ok {
		t.Error("space separated: got false, want true")
	}
	if ok, _ := Grade(q, want, "Mobile AL: 129937.96, 63242.15, 66695.81\nMemphis TN: 245822.83, 117428.82, 128394.01"); !ok {
		t.Error("shuffled multi rows: got false, want true")
	}
	if ok, _ := Grade(q, want, "Memphis TN: 245822.83, 117428.82\nMobile AL: 129937.96, 63242.15, 66695.81"); ok {
		t.Error("short value list: got true, want false")
	}
	if ok, _ := Grade(q, want, "Memphis TN: 245822.83, 117428.82, 999.0\nMobile AL: 129937.96, 63242.15, 66695.81"); ok {
		t.Error("wrong third value: got true, want false")
	}
	h08 := Question{ID: "h08", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	h08want := TableAnswer{
		Columns: []string{"month", "new_users", "net_revenue"},
		Rows: [][]string{
			{"2024-01", "2300", "167962.75"},
			{"2024-02", "2353", "165514.52"},
		},
	}
	if ok, _ := Grade(h08, h08want, "2024-01: 2300, 167962.75\n2024-02: 2353, 165514.52"); !ok {
		t.Error("h08 comma: got false, want true")
	}
	if ok, _ := Grade(h08, h08want, "2024-01: 2300 167962.75\n2024-02: 2353 165514.52"); !ok {
		t.Error("h08 space: got false, want true")
	}
}

func TestGradeTableStringCells(t *testing.T) {
	q := Question{ID: "m01", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	want := TableAnswer{
		Columns: []string{"label", "value"},
		Rows:    [][]string{{"alpha", "hello"}},
	}
	if ok, _ := Grade(q, want, "alpha: hello"); !ok {
		t.Error("string cell exact: got false, want true")
	}
	if ok, _ := Grade(q, want, "ALPHA: Hello"); !ok {
		t.Error("string cell case: got false, want true")
	}
	if ok, _ := Grade(q, want, "alpha: bye"); ok {
		t.Error("string cell mismatch: got true, want false")
	}
}

func TestGradeTableSingleColumn(t *testing.T) {
	tol := 0.01
	q := Question{ID: "m01", AnswerType: TypeTable, Tolerance: &tol}
	want := TableAnswer{
		Columns: []string{"name"},
		Rows:    [][]string{{"a"}, {"b"}},
	}
	if ok, _ := Grade(q, want, "b\na"); !ok {
		t.Error("single column set: got false, want true")
	}
	if ok, _ := Grade(q, want, "a"); ok {
		t.Error("single column short: got true, want false")
	}
}

func TestGradeTableFromJSON(t *testing.T) {
	q := Question{ID: "m03", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	raw := map[string]any{
		"columns": []any{"department", "return_rate"},
		"rows": []any{
			[]any{"Men", "0.01006"},
			[]any{"Women", "0.01022"},
		},
	}
	if ok, err := Grade(q, raw, "Men: 0.01006\nWomen: 0.01022"); err != nil || !ok {
		t.Errorf("map table: got %v, %v; want true, nil", ok, err)
	}
	if _, err := Grade(q, "nope", "Men: 0.01"); err == nil {
		t.Error("string expected for table: want error")
	}
}

func TestGradeBadType(t *testing.T) {
	q := Question{ID: "e01", AnswerType: "paragraph"}
	if _, err := Grade(q, "x", "x"); err == nil {
		t.Error("bad answer_type: want error")
	}
	free := Question{ID: "f01", AnswerType: TypeFreeText}
	if _, err := Grade(free, "x", "x"); err == nil {
		t.Error("free_text Grade: want judge-required error")
	}
}

func TestNormalizeExpected(t *testing.T) {
	numQ := Question{AnswerType: TypeNumber}
	v, err := NormalizeExpected(numQ, 10.0)
	if err != nil || v.(float64) != 10 {
		t.Errorf("number = %v, %v", v, err)
	}
	strQ := Question{AnswerType: TypeString}
	v, err = NormalizeExpected(strQ, "hi")
	if err != nil || v.(string) != "hi" {
		t.Errorf("string = %v, %v", v, err)
	}
	freeQ := Question{AnswerType: TypeFreeText}
	if _, err := NormalizeExpected(freeQ, "hi"); err != nil {
		t.Errorf("free_text = %v", err)
	}
	listQ := Question{AnswerType: TypeList}
	v, err = NormalizeExpected(listQ, []any{"a", "b"})
	if err != nil || len(v.([]string)) != 2 {
		t.Errorf("list = %v, %v", v, err)
	}
	tabQ := Question{AnswerType: TypeTable}
	v, err = NormalizeExpected(tabQ, TableAnswer{Columns: []string{"a"}, Rows: [][]string{{"1"}}})
	if err != nil {
		t.Errorf("table = %v", err)
	}
	badQ := Question{AnswerType: "paragraph"}
	if _, err := NormalizeExpected(badQ, "x"); err == nil {
		t.Error("bad type: want error")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	entries := []SnapshotEntry{
		{ID: "e03", Answer: 10.0, ComputedAt: "2026-09-24T02:12:35Z"},
		{ID: "e01", Answer: "a@b.com", ComputedAt: "2026-09-24T02:12:35Z"},
		{ID: "e08", Answer: []string{"Cancelled", "Complete"}, ComputedAt: "2026-09-24T02:12:35Z"},
		{ID: "m01", Answer: TableAnswer{Columns: []string{"c", "r"}, Rows: [][]string{{"Jeans", "1.5"}}}, ComputedAt: "2026-09-24T02:12:35Z"},
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "answers.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSnapshot(path)
	if err != nil {
		t.Fatal(err)
	}
	qs := []Question{
		{ID: "e03", AnswerType: TypeNumber},
		{ID: "e01", AnswerType: TypeString},
		{ID: "e08", AnswerType: TypeList},
		{ID: "m01", AnswerType: TypeTable},
	}
	byID, err := ExpectedByID(loaded, qs)
	if err != nil {
		t.Fatal(err)
	}
	if byID["e03"].(float64) != 10 {
		t.Errorf("e03 = %v", byID["e03"])
	}
	if byID["e01"].(string) != "a@b.com" {
		t.Errorf("e01 = %v", byID["e01"])
	}
	if len(byID["e08"].([]string)) != 2 {
		t.Errorf("e08 = %v", byID["e08"])
	}
	if len(byID["m01"].(TableAnswer).Rows) != 1 {
		t.Errorf("m01 = %v", byID["m01"])
	}
	if _, err := ExpectedByID(loaded, []Question{{ID: "zzz", AnswerType: TypeString}}); err == nil {
		t.Error("missing id: want error")
	}
	if _, err := LoadSnapshot(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Error("missing file: want error")
	}
}

type fakeJudgeCaller struct {
	text string
	rec  llm.CallRecord
	err  error
	req  llm.Request
}

func (f *fakeJudgeCaller) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	f.req = req
	if f.err != nil {
		return nil, f.rec, f.err
	}
	msg := &anthropic.Message{
		Content: []anthropic.ContentBlockUnion{
			{Type: "text", Text: f.text},
		},
	}
	return msg, f.rec, nil
}

func TestJudgeFreeText(t *testing.T) {
	q := Question{ID: "f01", AnswerType: TypeFreeText, Text: "Summarize sales."}
	caller := &fakeJudgeCaller{
		text: "CORRECT\nThe answer matches the ground truth.",
		rec:  llm.CallRecord{Model: llm.ModelHaiku45, CostUSD: 0.0001, Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}},
	}
	res, err := JudgeFreeText(context.Background(), caller, q, "sales rose", "Sales rose last year.")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Correct || res.CostUSD != 0.0001 || res.Model != llm.ModelHaiku45 {
		t.Fatalf("result %+v", res)
	}
	if caller.req.Tier != llm.TierCheap {
		t.Errorf("judge tier = %s, want cheap", caller.req.Tier)
	}
	rawReq, _ := json.Marshal(caller.req)
	if !strings.Contains(string(rawReq), "sales rose") {
		t.Error("judge prompt lacks ground truth")
	}
	neg := &fakeJudgeCaller{text: "INCORRECT\nThe numbers differ."}
	res, err = JudgeFreeText(context.Background(), neg, q, "sales rose", "sales fell")
	if err != nil || res.Correct {
		t.Errorf("incorrect verdict = %+v, %v", res, err)
	}
	badCaller := &fakeJudgeCaller{err: context.DeadlineExceeded}
	if _, err := JudgeFreeText(context.Background(), badCaller, q, "sales rose", "x"); err == nil {
		t.Error("caller error: want error")
	}
	if _, err := JudgeFreeText(context.Background(), caller, q, 42.0, "x"); err == nil {
		t.Error("numeric expected: want error")
	}
}

func TestParseJudgeVerdict(t *testing.T) {
	cases := []struct {
		text    string
		want    bool
		wantErr bool
	}{
		{"CORRECT", true, false},
		{"correct: matches", true, false},
		{"**Correct.**\nThe totals match.", true, false},
		{"  INCORRECT", false, false},
		{"incorrect, wrong value", false, false},
		{"NOT CORRECT", false, true},
		{"The submitted answer is wrong, so this is incorrect", false, true},
		{"The answer is correct", false, true},
		{"Correct? No", false, true},
		{"Correctly answered", false, true},
		{"unrelated verdict", false, true},
		{"", false, true},
		{"\nCORRECT", true, false},
	}
	for _, c := range cases {
		got, err := parseJudgeVerdict(c.text)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%q = %v, %v; want %v, error %v", c.text, got, err, c.want, c.wantErr)
		}
	}
}

func TestJudgeFreeTextUnparsedVerdictKeepsCost(t *testing.T) {
	q := Question{ID: "f01", Text: "How did sales change?", AnswerType: TypeFreeText}
	caller := &fakeJudgeCaller{text: "NOT CORRECT", rec: llm.CallRecord{CostUSD: 0.002}}
	res, err := JudgeFreeText(context.Background(), caller, q, "sales rose", "sales fell")
	if err == nil || res.Correct {
		t.Fatalf("unparsed verdict = %+v, %v; want error", res, err)
	}
	if res.CostUSD == 0 {
		t.Errorf("unparsed verdict dropped the judge cost: %+v", res)
	}
}

func TestWithinTolerance(t *testing.T) {
	if !WithinTolerance(100, 100, 0) || WithinTolerance(100.1, 100, 0) {
		t.Error("zero tolerance")
	}
	if !WithinTolerance(101, 100, 0.01) || WithinTolerance(101.01, 100, 0.01) {
		t.Error("one percent boundary")
	}
	if WithinTolerance(0.5, 0, 0.01) || !WithinTolerance(0, 0, 0.01) {
		t.Error("zero want")
	}
}

func TestGradeLenientListLabelValue(t *testing.T) {
	listQ := Question{ID: "e08", AnswerType: TypeList}
	want := []string{"Cancelled", "Complete", "Returned"}
	if ok, _ := Grade(listQ, want, "Cancelled: 10\nComplete: 20\nReturned: 5"); !ok {
		t.Error("list label value: got false, want true")
	}
	if ok, _ := Grade(listQ, want, "Returned: 5\nCancelled: 10\nComplete: 20"); !ok {
		t.Error("list label value shuffled: got false, want true")
	}
	if ok, _ := GradeStrict(listQ, want, "Cancelled: 10\nComplete: 20\nReturned: 5"); ok {
		t.Error("list label value strict: got true, want false")
	}
	rankedQ := Question{ID: "m02", AnswerType: TypeRankedList}
	rankedWant := []string{"Levi's", "Dockers", "Allegra K"}
	if ok, _ := Grade(rankedQ, rankedWant, "Levi's: 3013\nDockers: 1289\nAllegra K: 1263"); !ok {
		t.Error("ranked label value: got false, want true")
	}
	if ok, _ := Grade(rankedQ, rankedWant, "Dockers: 1289\nLevi's: 3013\nAllegra K: 1263"); ok {
		t.Error("ranked label value swapped: got true, want false")
	}
	if ok, _ := GradeStrict(rankedQ, rankedWant, "Levi's: 3013\nDockers: 1289\nAllegra K: 1263"); ok {
		t.Error("ranked label value strict: got true, want false")
	}
	colonWant := []string{"a: 1", "b: 2"}
	if ok, _ := Grade(listQ, colonWant, "a: 1\nb: 2"); !ok {
		t.Error("colon expected exact: got false, want true")
	}
	if ok, _ := Grade(listQ, colonWant, "a\nb"); ok {
		t.Error("colon expected label only: got true, want false")
	}
}

func TestGradeLenientParenthetical(t *testing.T) {
	strQ := Question{ID: "h02", AnswerType: TypeString}
	want := "Dockers Mens Comfort Waist Khaki D3 Classic Fit Flat Front Pant"
	if ok, _ := Grade(strQ, want, "Dockers Mens Comfort Waist Khaki D3 Classic Fit Flat Front Pant (product id 21660, return rate 0.0278)"); !ok {
		t.Error("string parenthetical: got false, want true")
	}
	if ok, _ := Grade(strQ, want, "\"Dockers Mens Comfort Waist Khaki D3 Classic Fit Flat Front Pant (note)\""); !ok {
		t.Error("string quotes parenthetical: got false, want true")
	}
	if ok, _ := GradeStrict(strQ, want, "Dockers Mens Comfort Waist Khaki D3 Classic Fit Flat Front Pant (product id 21660)"); ok {
		t.Error("string parenthetical strict: got true, want false")
	}
	if ok, _ := Grade(strQ, "Facebook", "  'facebook'  "); !ok {
		t.Error("string quotes case space: got false, want true")
	}
	listQ := Question{ID: "e08", AnswerType: TypeList}
	listWant := []string{"Cancelled", "Complete"}
	if ok, _ := Grade(listQ, listWant, "Cancelled (10 items)\nComplete (20 items)"); !ok {
		t.Error("list parenthetical: got false, want true")
	}
	if ok, _ := GradeStrict(listQ, listWant, "Cancelled (10 items)\nComplete (20 items)"); ok {
		t.Error("list parenthetical strict: got true, want false")
	}
	rankedQ := Question{ID: "m02", AnswerType: TypeRankedList}
	rankedWant := []string{"Levi's", "Dockers"}
	if ok, _ := Grade(rankedQ, rankedWant, "Levi's (3013 sold)\nDockers (1289 sold)"); !ok {
		t.Error("ranked parenthetical: got false, want true")
	}
}

func TestGradeLenientMonthLabels(t *testing.T) {
	tabQ := Question{ID: "m07", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	tabWant := TableAnswer{
		Columns: []string{"month", "orders"},
		Rows: [][]string{
			{"2024-01", "3417"},
			{"2024-02", "3468"},
		},
	}
	if ok, _ := Grade(tabQ, tabWant, "2024-01-01: 3417\n2024-02-01: 3468"); !ok {
		t.Error("table month date: got false, want true")
	}
	if ok, _ := Grade(tabQ, tabWant, "2024-01-01 00:00:00: 3417\n2024-02-01 00:00:00: 3468"); !ok {
		t.Error("table month datetime: got false, want true")
	}
	if ok, _ := Grade(tabQ, tabWant, "2024-01-01: 3417\n2024-01-15: 3468"); ok {
		t.Error("table month duplicate prefix: got true, want false")
	}
	if ok, _ := GradeStrict(tabQ, tabWant, "2024-01-01: 3417\n2024-02-01: 3468"); ok {
		t.Error("table month strict: got true, want false")
	}
	listQ := Question{ID: "e08", AnswerType: TypeList}
	listWant := []string{"2024-01", "2024-02"}
	if ok, _ := Grade(listQ, listWant, "2024-01-01\n2024-02-01"); !ok {
		t.Error("list month date: got false, want true")
	}
	if ok, _ := Grade(listQ, listWant, "2024-02-01 00:00:00\n2024-01-01 00:00:00"); !ok {
		t.Error("list month datetime shuffled: got false, want true")
	}
	if ok, _ := GradeStrict(listQ, listWant, "2024-01-01\n2024-02-01"); ok {
		t.Error("list month strict: got true, want false")
	}
	rankedQ := Question{ID: "m02", AnswerType: TypeRankedList}
	if ok, _ := Grade(rankedQ, listWant, "2024-01-01\n2024-02-01"); !ok {
		t.Error("ranked month date: got false, want true")
	}
}

func TestGradeLenientTableValues(t *testing.T) {
	q := Question{ID: "h08", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	want := TableAnswer{
		Columns: []string{"month", "new_users", "net_revenue"},
		Rows: [][]string{
			{"2024-01", "2300", "167962.75"},
			{"2024-02", "2353", "165514.52"},
		},
	}
	if ok, _ := Grade(q, want, "2024-01: 2300 users, 167962.75 net revenue\n2024-02: 2353 users, 165514.52 net revenue"); !ok {
		t.Error("table units words: got false, want true")
	}
	if ok, _ := Grade(q, want, "2024-01: users=2300, net_revenue=167962.75\n2024-02: users=2353, net_revenue=165514.52"); !ok {
		t.Error("table equals labels: got false, want true")
	}
	if ok, _ := Grade(q, want, "2024-01: 2300\n2024-02: 2353, 165514.52"); ok {
		t.Error("table short numbers: got true, want false")
	}
	if ok, _ := Grade(q, want, "2024-01: 2300, 167962.75, 999\n2024-02: 2353, 165514.52"); ok {
		t.Error("table extra number: got true, want false")
	}
	if ok, _ := Grade(q, want, "2024-01: 2300 users, 100.00 net revenue\n2024-02: 2353 users, 165514.52 net revenue"); ok {
		t.Error("table wrong second number: got true, want false")
	}
	if ok, _ := GradeStrict(q, want, "2024-01: 2300 users, 167962.75 net revenue\n2024-02: 2353 users, 165514.52 net revenue"); ok {
		t.Error("table units strict: got true, want false")
	}
}

func TestGradeLenientMonthNames(t *testing.T) {
	tabQ := Question{ID: "m07", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	tabWant := TableAnswer{
		Columns: []string{"month", "orders"},
		Rows: [][]string{
			{"2024-01", "3417"},
			{"2024-02", "3468"},
			{"2024-03", "3802"},
			{"2024-04", "3869"},
			{"2024-05", "3925"},
			{"2024-06", "3980"},
			{"2024-07", "4246"},
			{"2024-08", "4324"},
			{"2024-09", "4695"},
			{"2024-10", "4893"},
			{"2024-11", "5430"},
			{"2024-12", "5725"},
		},
	}
	exact := "January: 3417\nFebruary: 3468\nMarch: 3802\nApril: 3869\nMay: 3925\nJune: 3980\nJuly: 4246\nAugust: 4324\nSeptember: 4695\nOctober: 4893\nNovember: 5430\nDecember: 5725"
	if ok, _ := Grade(tabQ, tabWant, exact); !ok {
		t.Error("table month names: got false, want true")
	}
	abbrev := "Jan: 3417\nFeb: 3468\nMar: 3802\nApr: 3869\nMay: 3925\nJun: 3980\nJul: 4246\nAug: 4324\nSep: 4695\nOct: 4893\nNov: 5430\nDec: 5725"
	if ok, _ := Grade(tabQ, tabWant, abbrev); !ok {
		t.Error("table month abbrev: got false, want true")
	}
	mixed := "january: 3417\nFEBRUARY: 3468\nMar.: 3802\napril 2024: 3869\n2024-05: 3925\nJune: 3980\nJuly: 4246\nAugust: 4324\nSept: 4695\nOctober: 4893\nNovember: 5430\nDecember: 5725"
	if ok, _ := Grade(tabQ, tabWant, mixed); !ok {
		t.Error("table month mixed forms: got false, want true")
	}
	wrongValue := "January: 3417\nFebruary: 3468\nMarch: 3802\nApril: 3869\nMay: 3925\nJune: 3980\nJuly: 4246\nAugust: 4324\nSeptember: 4695\nOctober: 4893\nNovember: 5430\nDecember: 100"
	if ok, _ := Grade(tabQ, tabWant, wrongValue); ok {
		t.Error("table month wrong value: got true, want false")
	}
	duplicate := "January: 3417\nJanuary: 3468\nMarch: 3802\nApril: 3869\nMay: 3925\nJune: 3980\nJuly: 4246\nAugust: 4324\nSeptember: 4695\nOctober: 4893\nNovember: 5430\nDecember: 5725"
	if ok, _ := Grade(tabQ, tabWant, duplicate); ok {
		t.Error("table month duplicate: got true, want false")
	}
	wrongYear := "January 2023: 3417\nFebruary: 3468\nMarch: 3802\nApril: 3869\nMay: 3925\nJune: 3980\nJuly: 4246\nAugust: 4324\nSeptember: 4695\nOctober: 4893\nNovember: 5430\nDecember: 5725"
	if ok, _ := Grade(tabQ, tabWant, wrongYear); ok {
		t.Error("table month wrong year: got true, want false")
	}
	if ok, _ := GradeStrict(tabQ, tabWant, exact); ok {
		t.Error("table month names strict: got true, want false")
	}
	smallQ := Question{ID: "m07", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	smallWant := TableAnswer{
		Columns: []string{"month", "orders"},
		Rows: [][]string{
			{"2024-01", "3417"},
			{"2024-02", "3468"},
		},
	}
	if ok, _ := Grade(smallQ, smallWant, "January: 3417\nFebruary: 3468"); !ok {
		t.Error("table month names small: got false, want true")
	}
	multiWant := TableAnswer{
		Columns: []string{"month", "orders"},
		Rows: [][]string{
			{"2023-12", "100"},
			{"2024-01", "3417"},
		},
	}
	if ok, _ := Grade(smallQ, multiWant, "December: 100\nJanuary: 3417"); ok {
		t.Error("table month names multi year: got true, want false")
	}
	nonMonthWant := TableAnswer{
		Columns: []string{"category", "revenue"},
		Rows: [][]string{
			{"Jeans", "382665.62"},
			{"Accessories", "232113.37"},
		},
	}
	if ok, _ := Grade(smallQ, nonMonthWant, "Jeans: 382665.62\nAccessories: 232113.37"); !ok {
		t.Error("table non month still exact: got false, want true")
	}
	listQ := Question{ID: "e08", AnswerType: TypeList}
	listWant := []string{"2024-01", "2024-02", "2024-03"}
	if ok, _ := Grade(listQ, listWant, "March\nJanuary\nFebruary"); !ok {
		t.Error("list month names: got false, want true")
	}
	if ok, _ := Grade(listQ, listWant, "Jan\nFeb\nMar"); !ok {
		t.Error("list month abbrev: got false, want true")
	}
	if ok, _ := GradeStrict(listQ, listWant, "January\nFebruary\nMarch"); ok {
		t.Error("list month names strict: got true, want false")
	}
	rankedQ := Question{ID: "m02", AnswerType: TypeRankedList}
	if ok, _ := Grade(rankedQ, listWant, "January\nFebruary\nMarch"); !ok {
		t.Error("ranked month names: got false, want true")
	}
	if ok, _ := Grade(rankedQ, listWant, "February\nJanuary\nMarch"); ok {
		t.Error("ranked month names swapped: got true, want false")
	}
}

func TestGradeStrictMatchesLegacy(t *testing.T) {
	strQ := Question{ID: "e01", AnswerType: TypeString}
	if ok, _ := GradeStrict(strQ, "Facebook", "facebook"); !ok {
		t.Error("strict string case: got false, want true")
	}
	if ok, _ := GradeStrict(strQ, "Facebook", "Search"); ok {
		t.Error("strict string mismatch: got true, want false")
	}
	listQ := Question{ID: "e08", AnswerType: TypeList}
	if ok, _ := GradeStrict(listQ, []string{"a", "b"}, "b\na"); !ok {
		t.Error("strict list set: got false, want true")
	}
	tabQ := Question{ID: "m01", AnswerType: TypeTable, Tolerance: tolPtr(0.01)}
	tabWant := TableAnswer{Columns: []string{"c", "r"}, Rows: [][]string{{"Jeans", "100"}}}
	if ok, _ := GradeStrict(tabQ, tabWant, "Jeans: 100"); !ok {
		t.Error("strict table exact: got false, want true")
	}
	if ok, _ := GradeStrict(tabQ, tabWant, "Jeans: revenue 100"); ok {
		t.Error("strict table words: got true, want false")
	}
}

func TestGradeStrictListDuplicates(t *testing.T) {
	q := Question{ID: "e08", AnswerType: TypeList}
	want := []any{"Complete", "Returned"}
	for _, got := range []string{"Complete\nComplete\nReturned", "Complete\nComplete"} {
		strict, err := GradeStrict(q, want, got)
		if err != nil {
			t.Fatal(err)
		}
		lenient, err := Grade(q, want, got)
		if err != nil {
			t.Fatal(err)
		}
		if strict || lenient {
			t.Errorf("%q: strict %v lenient %v, want both false", got, strict, lenient)
		}
	}
	if ok, _ := GradeStrict(q, []any{"A", "A", "B"}, "a\nB\nA"); !ok {
		t.Error("matching duplicates: want pass")
	}
	if ok, _ := GradeStrict(q, []any{"A", "A", "B"}, "A\nB\nB"); ok {
		t.Error("different duplicate counts: want fail")
	}
	if ok, _ := GradeStrict(q, want, "returned\n Complete "); !ok {
		t.Error("reordered set: want pass")
	}
}

func TestGradeLenientStringParentheticalOneSided(t *testing.T) {
	q := Question{ID: "h06", AnswerType: TypeString}
	cases := []struct {
		want, got string
		pass      bool
	}{
		{"Widget (Red)", "Widget (Blue)", false},
		{"Widget (Red)", "Widget", false},
		{"Widget (Red)", "widget (red)", true},
		{"Widget", "Widget (Blue)", true},
		{"Widget", "\"Widget (42 orders)\"", true},
		{"Widget", "Gadget (Widget)", false},
	}
	for _, c := range cases {
		ok, err := Grade(q, c.want, c.got)
		if err != nil {
			t.Fatal(err)
		}
		if ok != c.pass {
			t.Errorf("want %q got %q: pass %v, want %v", c.want, c.got, ok, c.pass)
		}
	}
}

func TestGradeNumberThousandsSeparators(t *testing.T) {
	q := Question{ID: "e04", AnswerType: TypeNumber}
	cases := []struct {
		got     string
		want    float64
		lenient bool
		strict  bool
	}{
		{"1,968", 1968, true, false},
		{"$1,968.00 in total", 1968, true, false},
		{"There were 1,968 orders", 1968, true, false},
		{"$382,665.62", 382665.62, true, false},
		{"-$1,234", -1234, true, false},
		{"1968", 1968, true, true},
		{"12,3456", 12, true, true},
		{"1,96", 1, true, true},
		{"3 of 1,968", 3, true, true},
	}
	for _, c := range cases {
		lenient, err := Grade(q, c.want, c.got)
		if err != nil {
			t.Fatal(err)
		}
		strict, err := GradeStrict(q, c.want, c.got)
		if err != nil {
			t.Fatal(err)
		}
		if lenient != c.lenient || strict != c.strict {
			t.Errorf("%q vs %v: lenient %v strict %v, want %v %v", c.got, c.want, lenient, strict, c.lenient, c.strict)
		}
	}
	l := Line{Submitted: true, Answer: "1,968", Expected: 1968.0, Correct: true}
	if fail, format := Explain(q, l); fail != "" || !strings.HasPrefix(format, FormatNumberFormat+":") {
		t.Errorf("explain %q %q", fail, format)
	}
	if r := FailReason(q, Line{Submitted: true, Answer: "1,900", Expected: 1968.0}); !strings.Contains(r, "got 1900,") {
		t.Errorf("fail reason %q", r)
	}
}

func TestGradeLenientTableCSV(t *testing.T) {
	tol := 0.01
	q := Question{ID: "m01", AnswerType: TypeTable, Tolerance: &tol}
	revenue := TableAnswer{Columns: []string{"category", "revenue"}, Rows: [][]string{
		{"Jeans", "382665.6215791702"}, {"Accessories", "232113.37109065056"}, {"Outerwear & Coats", "207024.4900341034"},
	}}
	cohort := TableAnswer{Columns: []string{"month", "new_users", "net_revenue"}, Rows: [][]string{
		{"2024-01", "2300", "167962.7505016327"}, {"2024-02", "2353", "165514.5206129551"}, {"2024-03", "2558", "168568.44048762321"},
	}}
	orders := TableAnswer{Columns: []string{"month", "orders"}, Rows: [][]string{{"2024-01", "3417"}, {"2024-02", "3468"}, {"2024-03", "3802"}}}
	cases := []struct {
		name string
		want TableAnswer
		got  string
		pass bool
	}{
		{"m01 header", revenue, "category,gross_revenue\nJeans,382665.62\nAccessories,232113.37\nOuterwear & Coats,207024.49", true},
		{"no header", revenue, "Jeans,382665.62\nAccessories,232113.37\nOuterwear & Coats,207024.49", true},
		{"quoted label", revenue, "category,revenue\n\"Jeans\",382665.62\nAccessories, 232113.37\n\"Outerwear & Coats\",\"207,024.49\"", true},
		{"wrong value", revenue, "category,gross_revenue\nJeans,382665.62\nAccessories,132113.37\nOuterwear & Coats,207024.49", false},
		{"extra row", revenue, "category,gross_revenue\nJeans,382665.62\nAccessories,232113.37\nOuterwear & Coats,207024.49\nSwim,71909.73", false},
		{"header only counts once", revenue, "category,revenue\ncategory,revenue\nJeans,382665.62\nAccessories,232113.37", false},
		{"h08 dates", cohort, "acquisition_month,new_users,net_revenue_2024\n2024-01-01,2300,167962.75\n2024-02-01,2353,165514.52\n2024-03-01,2558,168568.44", true},
		{"h08 wrong users", cohort, "acquisition_month,new_users,net_revenue_2024\n2024-01-01,2400,167962.75\n2024-02-01,2353,165514.52\n2024-03-01,2558,168568.44", false},
		{"m07 datetimes", orders, "2024-01-01 00:00:00,3417\n2024-02-01 00:00:00,3468\n2024-03-01 00:00:00,3802", true},
		{"m07 column header", orders, "month,orders\n2024-01,3417\n2024-02,3468\n2024-03,3802", true},
		{"m07 colon dates", orders, "2024-01-01: 3417\n2024-02-01: 3468\n2024-03-01: 3802", true},
	}
	for _, c := range cases {
		ok, err := Grade(q, c.want, c.got)
		if err != nil {
			t.Fatal(err)
		}
		if ok != c.pass {
			t.Errorf("%s: lenient %v, want %v", c.name, ok, c.pass)
		}
		strict, err := GradeStrict(q, c.want, c.got)
		if err != nil {
			t.Fatal(err)
		}
		if strict && strings.Contains(c.got, ",") {
			t.Errorf("%s: strict passed a CSV answer", c.name)
		}
		l := Line{Submitted: true, Answer: c.got, Expected: c.want, Correct: ok, CorrectStrict: strict}
		fail, format := Explain(q, l)
		if ok && (fail != "" || !strings.HasPrefix(format, FormatCSVRows+":") && strings.Contains(c.got, ",")) {
			t.Errorf("%s: passing line explained as fail %q format %q", c.name, fail, format)
		}
		if !ok && (fail == "" || ReasonCode(fail) == ReasonStaleVerdict) {
			t.Errorf("%s: failing line fail reason %q", c.name, fail)
		}
	}
}

func TestGradeCommittedCSVAnswers(t *testing.T) {
	qs, err := Load(filepath.Join("..", "..", BenchDir, QuestionsFile))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Question{}
	for _, q := range qs {
		byID[q.ID] = q
	}
	cases := []struct {
		path string
		id   string
	}{
		{filepath.Join("..", "..", "results", "always-mid", "20260924T162945Z.jsonl"), "m01"},
		{filepath.Join("..", "..", "results", "always-mid", "20260924T162945Z.jsonl"), "h08"},
		{filepath.Join("..", "..", "results", "venice", "always-mid", "20260924T064920Z.jsonl"), "m07"},
	}
	for _, c := range cases {
		lines, err := ReadLines(c.path)
		if err != nil {
			t.Skipf("committed run not present: %v", err)
		}
		found := false
		for _, l := range lines {
			if l.ID != c.id {
				continue
			}
			found = true
			q := byID[c.id]
			expected, err := NormalizeExpected(q, l.Expected)
			if err != nil {
				t.Fatal(err)
			}
			ok, err := Grade(q, expected, l.Answer)
			if err != nil || !ok {
				t.Errorf("%s %s: lenient %v %v, want pass", c.path, c.id, ok, err)
			}
			strict, err := GradeStrict(q, expected, l.Answer)
			if err != nil || strict {
				t.Errorf("%s %s: strict %v %v, want fail", c.path, c.id, strict, err)
			}
			l.Expected = expected
			l.Correct, l.CorrectStrict = ok, strict
			if fail, format := Explain(q, l); fail != "" || ReasonCode(format) != FormatCSVRows {
				t.Errorf("%s %s: fail %q format %q", c.path, c.id, fail, format)
			}
		}
		if !found {
			t.Errorf("%s has no %s line", c.path, c.id)
		}
	}
}
