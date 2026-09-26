package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStarterSet(t *testing.T) {
	dir := filepath.Join("..", "..", BenchDir)
	qs, err := Load(filepath.Join(dir, QuestionsFile))
	if err != nil {
		t.Fatal(err)
	}
	set, err := LoadSet(SetPath(dir, SetStarter))
	if err != nil {
		t.Fatal(err)
	}
	got, err := SelectSet(qs, set)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 10 || set.Name != SetStarter {
		t.Fatalf("starter set has %d questions, name %q", len(got), set.Name)
	}
	for _, q := range got {
		if q.Difficulty == DifficultyExpert {
			t.Errorf("starter set includes expert question %s", q.ID)
		}
	}
	for _, e := range set.Questions {
		if strings.TrimSpace(e.Why) == "" {
			t.Errorf("%s has no why", e.ID)
		}
	}
	if _, err := SelectSet(qs, QuestionSet{Name: "bad", Questions: []SetEntry{{ID: "z99"}}}); err == nil {
		t.Error("unknown id: want error")
	}
	if _, err := SelectSet(qs, QuestionSet{Name: "dup", Questions: []SetEntry{{ID: "e01"}, {ID: "e01"}}}); err == nil {
		t.Error("duplicate id: want error")
	}
}

func TestAnnotateRawLine(t *testing.T) {
	raw := `{"id":"e04","correct":false,"correct_strict":false,"answer":"1900","z":1}`
	out, err := AnnotateRawLine([]byte(raw), `wrong_number: got "1900"`, "")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"e04","correct":false,"correct_strict":false,"fail_reason":"wrong_number: got \"1900\"","answer":"1900","z":1}`
	if string(out) != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
	again, err := AnnotateRawLine(out, "no_answer: x", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != `{"id":"e04","correct":false,"correct_strict":false,"fail_reason":"no_answer: x","answer":"1900","z":1}` {
		t.Fatalf("re-annotate: %s", again)
	}
	cleared, err := AnnotateRawLine(again, "", "")
	if err != nil || string(cleared) != raw {
		t.Fatalf("clear: %s %v", cleared, err)
	}
	old := `{"id":"e04","correct":true,"answer":"1968"}`
	out, err = AnnotateRawLine([]byte(old), "", "count_appended: x")
	if err != nil || string(out) != `{"id":"e04","correct":true,"format_reason":"count_appended: x","answer":"1968"}` {
		t.Fatalf("pre-strict line: %s %v", out, err)
	}
	if _, err := AnnotateRawLine([]byte(`{"id":"e04"}`), "x", ""); err == nil {
		t.Error("line without verdict: want error")
	}
}

func TestRegradeFileKeepsOtherBytes(t *testing.T) {
	qs := []Question{{ID: "e04", Difficulty: DifficultyEasy, AnswerType: TypeNumber}, {ID: "m02", Difficulty: DifficultyModerate, AnswerType: TypeRankedList}}
	body := `{"id":"e04","correct":false,"correct_strict":false,"answer":"1900","expected":1968,"submitted":true,"cost_usd":0.1}
{"id":"m02","correct":true,"correct_strict":false,"answer":"Levi's: 3\nDockers: 2","expected":["Levi's","Dockers"],"submitted":true}
{"id":"m02","correct":true,"answer":"Levi's: 3\nDockers: 2","expected":["Levi's","Dockers"],"submitted":true}
`
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := RegradeFile(path, qs)
	if err != nil {
		t.Fatal(err)
	}
	if ReasonCode(lines[0].FailReason) != ReasonWrongNumber || ReasonCode(lines[1].FormatReason) != FormatCountAppended || lines[2].FormatReason != "" {
		t.Fatalf("reasons %q %q %q", lines[0].FailReason, lines[1].FormatReason, lines[2].FormatReason)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RegradeFile(path, qs); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("regrade is not idempotent:\n%s\n%s", first, second)
	}
	var stripped []string
	for _, l := range splitJSONLines(string(second)) {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		delete(m, "fail_reason")
		delete(m, "format_reason")
		b, _ := json.Marshal(m)
		stripped = append(stripped, string(b))
	}
	for i, l := range splitJSONLines(body) {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(m)
		if string(b) != stripped[i] {
			t.Errorf("line %d changed beyond the reason fields:\n%s\n%s", i+1, b, stripped[i])
		}
	}
}

func TestSetSummaryReasons(t *testing.T) {
	in := "{\n  \"config\": \"c\",\n  \"total\": 2,\n  \"omni\": {\n    \"questions\": 2\n  }\n}\n"
	out, err := SetSummaryReasons([]byte(in), map[string]int{"wrong_number": 1}, map[string]int{"unit_words": 2})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"config\": \"c\",\n  \"total\": 2,\n  \"fail_reasons\": {\n    \"wrong_number\": 1\n  },\n  \"format_reasons\": {\n    \"unit_words\": 2\n  },\n  \"omni\": {\n    \"questions\": 2\n  }\n}\n"
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	again, err := SetSummaryReasons(out, map[string]int{"wrong_number": 1}, map[string]int{"unit_words": 2})
	if err != nil || string(again) != want {
		t.Fatalf("not idempotent:\n%s %v", again, err)
	}
	cleared, err := SetSummaryReasons(out, nil, nil)
	if err != nil || string(cleared) != in {
		t.Fatalf("clear:\n%s %v", cleared, err)
	}
	plain := "{\n  \"config\": \"c\"\n}\n"
	out, err = SetSummaryReasons([]byte(plain), map[string]int{"no_answer": 1}, nil)
	if err != nil || string(out) != "{\n  \"config\": \"c\",\n  \"fail_reasons\": {\n    \"no_answer\": 1\n  }\n}\n" {
		t.Fatalf("no omni block:\n%s %v", out, err)
	}
}

func TestRegress(t *testing.T) {
	base := loadSide(t, "base", "regress_base.jsonl")
	cand := loadSide(t, "cand", "regress_cand.jsonl")
	r := Regress(base, cand, 0, 0)
	if len(r.Common) != 3 || r.BaselinePass != 2 || r.CandidatePass != 2 || r.Drop() != 0 {
		t.Fatalf("common %v pass %d/%d drop %d", r.Common, r.BaselinePass, r.CandidatePass, r.Drop())
	}
	if len(r.PassToFail) != 2 || r.PassToFail[0].ID != "e04" || r.PassToFail[1].ID != "h08" || r.PassToFail[1].Reason != ReasonMissingFromCandidate || len(r.FailToPass) != 1 || r.FailToPass[0].ID != "m07" {
		t.Fatalf("flips %+v %+v", r.PassToFail, r.FailToPass)
	}
	if len(r.Missing) != 1 || r.Missing[0] != "h08" {
		t.Fatalf("missing %v", r.Missing)
	}
	f := r.Failures()
	if len(f) != 1 || !strings.Contains(f[0], "pass-to-fail flips 2 (max 0)") {
		t.Fatalf("failures %v", f)
	}
	text := r.Format()
	for _, want := range []string{
		"questions in common: 3 (baseline 4, candidate 4)",
		"passing: baseline 2/3, candidate 2/3, drop 0 (max 0)",
		"e04 (easy)",
		`baseline:  pass answer "1968"`,
		`candidate: fail answer "1900", reason wrong_number: got 1900, want 1968`,
		`baseline:  fail answer "2024-01: 3417", reason wrong_row_labels`,
		"missing from candidate: 1 (h08)",
		"h08 (hard)\n  baseline:  pass answer \"2024-01: 2300, 167962.75\"\n  candidate: missing from candidate\n",
		"result: FAIL",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q\n%s", want, text)
		}
	}
	if f := Regress(base, cand, 0, 1).Failures(); len(f) != 1 {
		t.Errorf("max-flips 1: failures %v, want the missing h08 to count", f)
	}
	if f := Regress(base, cand, 0, 2).Failures(); len(f) != 0 {
		t.Errorf("max-flips 2: failures %v", f)
	}
	if !strings.Contains(Regress(base, cand, 0, 2).Format(), "result: PASS") {
		t.Error("max-flips 2: want PASS")
	}
	worse := Regress(cand, base, 0, 5)
	if worse.Drop() != 0 || len(worse.Failures()) != 0 {
		t.Errorf("reverse: drop %d failures %v", worse.Drop(), worse.Failures())
	}
	self := Regress(base, base, 0, 0)
	if len(self.Failures()) != 0 || len(self.PassToFail) != 0 {
		t.Errorf("self compare: %v", self.Failures())
	}
	var partial []Line
	for _, l := range base.Lines {
		if l.ID != "m04" && l.ID != "h08" {
			partial = append(partial, l)
		}
	}
	crashed := Regress(base, Side{Label: "partial", Lines: partial}, 0, 0)
	if crashed.Drop() != 0 || len(crashed.PassToFail) != 2 || len(crashed.Failures()) != 1 {
		t.Errorf("partial candidate: drop %d flips %+v failures %v", crashed.Drop(), crashed.PassToFail, crashed.Failures())
	}
	drop := Regress(base, Side{Label: "empty", Lines: []Line{{ID: "e04", Answer: "1"}, {ID: "m04", Answer: "2"}}}, 1, 5)
	if drop.Drop() != 2 || len(drop.Failures()) != 1 || !strings.Contains(drop.Failures()[0], "accuracy dropped by 2 shared questions (max 1)") {
		t.Errorf("drop: %d %v", drop.Drop(), drop.Failures())
	}
}

func TestDrift(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "drift", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var sides []Side
	for i := len(paths) - 1; i >= 0; i-- {
		lines, err := ReadLines(paths[i])
		if err != nil {
			t.Fatal(err)
		}
		sides = append(sides, NewSide(paths[i], paths[i], lines))
	}
	runs := Drift(sides)
	if len(runs) != 3 || runs[0].Stamp != "20260101T000000Z" || runs[2].Stamp != "20260103T000000Z" {
		t.Fatalf("runs not in date order: %+v", runs)
	}
	if len(runs[0].Changes) != 0 || runs[1].Added != 1 || runs[2].Removed != 1 {
		t.Errorf("added/removed: %+v", runs)
	}
	if len(runs[1].Changes) != 1 || runs[1].Changes[0] != (DriftChange{ID: "e04", From: "pass", To: "fail", Reason: "wrong_number: got 1900, want 1968, off by 3.46%, tolerance 1.00%"}) {
		t.Errorf("run 2 changes %+v", runs[1].Changes)
	}
	if len(runs[2].Changes) != 1 || runs[2].Changes[0].From != "fail" || runs[2].Changes[0].To != "pass" {
		t.Errorf("run 3 changes %+v", runs[2].Changes)
	}
	text := FormatDrift("testdata/drift", runs)
	for _, want := range []string{
		"drift for testdata/drift: 3 runs",
		"20260101T000000Z  questions=2 accuracy=1.000 (2/2) strict=2 cost=$0.003000",
		"20260102T000000Z  questions=3 accuracy=0.667 (2/3) strict=2 cost=$0.006000 changed=1 added=1",
		"    e04 pass -> fail: wrong_number",
		"20260103T000000Z  questions=2 accuracy=1.000 (2/2) strict=2 cost=$0.005000 changed=1 removed=1",
		"    e04 fail -> pass\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("drift output lacks %q\n%s", want, text)
		}
	}
}

func TestRegradeFileReplacesAtomically(t *testing.T) {
	qs := []Question{{ID: "e04", Difficulty: DifficultyEasy, AnswerType: TypeNumber}}
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	body := `{"id":"e04","correct":false,"correct_strict":false,"answer":"1900","expected":1968,"submitted":true}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RegradeFile(path, qs); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("regrade rewrote the file in place, want a temp file renamed over it")
	}
	if after.Mode().Perm() != 0o644 {
		t.Errorf("mode %v, want 0644", after.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"fail_reason":"wrong_number`) {
		t.Errorf("regraded file %s", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestAnnotateRawLineReplacesReasonsAnywhere(t *testing.T) {
	raw := `{"id":"e04","correct":false,"correct_strict":false,"ex_correct":true,"fail_reason":"stale","format_reason":"old fmt","answer":"1900","n":12}`
	out, err := AnnotateRawLine([]byte(raw), "fresh", "")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"e04","correct":false,"correct_strict":false,"fail_reason":"fresh","ex_correct":true,"answer":"1900","n":12}`
	if string(out) != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
	first := `{"fail_reason":"a", "id":"x","correct":true,"attempts":[{"correct":true,"fail_reason":"keep"}],"format_reason":"z"}`
	out, err = AnnotateRawLine([]byte(first), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"id":"x","correct":true,"attempts":[{"correct":true,"fail_reason":"keep"}]}` {
		t.Fatalf("leading and trailing: %s", out)
	}
	var l Line
	dup := `{"id":"x","correct":false,"fail_reason":"a","answer":"1","fail_reason":"b"}`
	out, err = AnnotateRawLine([]byte(dup), "c", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &l); err != nil || l.FailReason != "c" || strings.Count(string(out), "fail_reason") != 1 {
		t.Fatalf("duplicate keys: %s %v", out, err)
	}
}
