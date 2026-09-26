package bench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

func authorSemantic() *semantic.Semantic {
	return &semantic.Semantic{
		Schema:  "public",
		Dataset: "fixture",
		Tables: []semantic.Table{
			{Name: "users", Purpose: "p", Grain: "g", PrimaryKey: "id",
				Columns: []semantic.Column{{Name: "id", Type: "integer", Description: "d"}}},
			{Name: "order_items", Purpose: "p", Grain: "g", PrimaryKey: "id",
				Columns: []semantic.Column{{Name: "id", Type: "integer", Description: "d"}}},
		},
		Metrics: []semantic.Metric{
			{Name: "gross_revenue", Description: "Total dollars charged before returns.", StatusFilter: "x", SQL: "SELECT 1"},
		},
		Synonyms:    []semantic.Synonym{{Term: "customer", RefersTo: "users"}},
		Conventions: []string{"Query schema public only."},
		Gotchas:     []string{"Filter rows with WHERE on status."},
		Examples:    []semantic.Example{{Question: "How many users?", SQL: "SELECT COUNT(*) FROM users"}},
	}
}

func authorDiagnosis() (Question, Diagnosis) {
	q := diagQuestion("m02", TypeRankedList, "SELECT brand FROM t GROUP BY brand ORDER BY COUNT(*) DESC LIMIT 5")
	d := Diagnosis{ID: "m02", Difficulty: DifficultyModerate, Question: q.Text, AnswerType: TypeRankedList,
		Expected: []any{"a", "b"}, GroundSQL: q.SQL, AgentSQL: q.SQL, AgentAnswer: "a: 1\nb: 2",
		Lenient: true, Strict: false, FormatReason: "count_appended", Class: ClassFormat, Detail: "strict fails"}
	return q, d
}

func TestParseDraftReply(t *testing.T) {
	raw := "for_question: m02\nkind: gotcha\nlevel: model\ntext: Submit bare labels.\njustification: It fixes the format.\n"
	d, err := ParseDraftReply(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != DraftGotcha || d.Text != "Submit bare labels." {
		t.Fatalf("draft = %+v", d)
	}
	fenced := "Here is the draft:\n```yaml\n" + raw + "```\n"
	d, err = ParseDraftReply(fenced)
	if err != nil {
		t.Fatal(err)
	}
	if d.ForQuestion != "m02" {
		t.Fatalf("draft = %+v", d)
	}
	if _, err := ParseDraftReply("not: [valid"); err == nil {
		t.Fatal("invalid YAML: want error")
	}
	if _, err := ParseDraftReply(raw + "title: extra\n"); err == nil {
		t.Fatal("unknown field: want error")
	}
	if _, err := ParseDraftReply("- just\n- a\n- list\n"); err == nil {
		t.Fatal("list shape: want error")
	}
}

func TestValidateDraftKinds(t *testing.T) {
	sem := authorSemantic()
	valid := []Draft{
		{Kind: DraftGotcha, Level: semantic.LevelModel, Text: "Submit bare labels with no appended values.", Justification: "why"},
		{Kind: DraftSynonym, Level: semantic.LevelField, Term: "buyer", RefersTo: "users", Justification: "why"},
		{Kind: DraftSynonym, Level: semantic.LevelField, Term: "buyer id", RefersTo: "users.id", Justification: "why"},
		{Kind: DraftMetricNote, Level: semantic.LevelTopic, Metric: "gross_revenue", Note: "Counts shipped items too.", Justification: "why"},
		{Kind: DraftExample, Level: semantic.LevelTopic, Question: "What is an order?", SQL: "SELECT COUNT(*) FROM order_items", Justification: "why"},
	}
	for i, d := range valid {
		if err := d.Validate(sem); err != nil {
			t.Fatalf("valid[%d] (%s): %v", i, d.Kind, err)
		}
	}
	invalid := []Draft{
		{Kind: "rule", Level: semantic.LevelModel, Text: "x", Justification: "why"},
		{Kind: DraftGotcha, Level: "global", Text: "x", Justification: "why"},
		{Kind: DraftGotcha, Level: semantic.LevelModel, Text: "x"},
		{Kind: DraftGotcha, Level: semantic.LevelModel, Text: "", Justification: "why"},
		{Kind: DraftGotcha, Level: semantic.LevelModel, Text: "Filter rows with WHERE on status.", Justification: "why"},
		{Kind: DraftGotcha, Level: semantic.LevelModel, Text: "Query schema public only.", Justification: "why"},
		{Kind: DraftGotcha, Level: semantic.LevelModel, Text: strings.Repeat("x", MaxDraftText+1), Justification: "why"},
		{Kind: DraftSynonym, Level: semantic.LevelField, Term: "", RefersTo: "users", Justification: "why"},
		{Kind: DraftSynonym, Level: semantic.LevelField, Term: "x", RefersTo: "orders", Justification: "why"},
		{Kind: DraftSynonym, Level: semantic.LevelField, Term: "x", RefersTo: "users.email", Justification: "why"},
		{Kind: DraftSynonym, Level: semantic.LevelField, Term: "Customer", RefersTo: "users", Justification: "why"},
		{Kind: DraftMetricNote, Level: semantic.LevelTopic, Metric: "net_revenue", Note: "x", Justification: "why"},
		{Kind: DraftMetricNote, Level: semantic.LevelTopic, Metric: "gross_revenue", Note: "Total dollars charged before returns.", Justification: "why"},
		{Kind: DraftExample, Level: semantic.LevelTopic, Question: "", SQL: "SELECT 1", Justification: "why"},
		{Kind: DraftExample, Level: semantic.LevelTopic, Question: "q", SQL: "UPDATE users SET x = 1", Justification: "why"},
		{Kind: DraftExample, Level: semantic.LevelTopic, Question: "How many users?", SQL: "SELECT 1", Justification: "why"},
	}
	for i, d := range invalid {
		if err := d.Validate(sem); err == nil {
			t.Fatalf("invalid[%d] (%+v): want error", i, d)
		}
	}
}

func TestAuthorOneSuccess(t *testing.T) {
	sem := authorSemantic()
	q, d := authorDiagnosis()
	calls := 0
	call := func(ctx context.Context, purpose, prompt string) (AuthorReply, error) {
		calls++
		if purpose != "context-author-m02" {
			t.Fatalf("purpose = %q", purpose)
		}
		return AuthorReply{Text: "kind: gotcha\nlevel: model\ntext: Submit bare labels.\njustification: It fixes the format.\n",
			Model: "cheap-model", Usage: llm.Usage{InputTokens: 10, OutputTokens: 5}, CostUSD: 0.00001}, nil
	}
	draft, replies, err := AuthorOne(context.Background(), call, sem, q, d)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(replies) != 1 {
		t.Fatalf("calls = %d, replies = %d, want 1", calls, len(replies))
	}
	if draft.ForQuestion != "m02" || draft.Kind != DraftGotcha {
		t.Fatalf("draft = %+v", draft)
	}
	if !strings.Contains(draft.Summary(), "gotcha") || !strings.Contains(draft.Summary(), "model level") {
		t.Fatalf("summary =\n%s", draft.Summary())
	}
}

func TestAuthorOneRetry(t *testing.T) {
	sem := authorSemantic()
	q, d := authorDiagnosis()
	var prompts []string
	texts := []string{"not yaml: [", "kind: gotcha\nlevel: model\ntext: Submit bare labels.\njustification: ok\n"}
	call := func(ctx context.Context, purpose, prompt string) (AuthorReply, error) {
		prompts = append(prompts, prompt)
		reply := texts[len(prompts)-1]
		return AuthorReply{Text: reply, Model: "m"}, nil
	}
	draft, replies, err := AuthorOne(context.Background(), call, sem, q, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(replies) != 2 || draft.Text != "Submit bare labels." {
		t.Fatalf("replies = %d, draft = %+v", len(replies), draft)
	}
	if !strings.Contains(prompts[1], "failed validation") {
		t.Fatal("second prompt does not carry the validation error")
	}
}

func TestAuthorOneGivesUp(t *testing.T) {
	sem := authorSemantic()
	q, d := authorDiagnosis()
	call := func(ctx context.Context, purpose, prompt string) (AuthorReply, error) {
		return AuthorReply{Text: "kind: nope\nlevel: model\ntext: x\njustification: y\n", Model: "m"}, nil
	}
	_, replies, err := AuthorOne(context.Background(), call, sem, q, d)
	if err == nil || !strings.Contains(err.Error(), "m02") {
		t.Fatalf("err = %v, want error naming m02", err)
	}
	if len(replies) != MaxAttempts {
		t.Fatalf("replies = %d, want %d", len(replies), MaxAttempts)
	}
}

func TestAuthorOneCallError(t *testing.T) {
	sem := authorSemantic()
	q, d := authorDiagnosis()
	call := func(ctx context.Context, purpose, prompt string) (AuthorReply, error) {
		return AuthorReply{}, context.DeadlineExceeded
	}
	_, _, err := AuthorOne(context.Background(), call, sem, q, d)
	if err == nil || !strings.Contains(err.Error(), "m02") {
		t.Fatalf("err = %v, want error naming m02", err)
	}
}

func TestDraftFileRoundTrip(t *testing.T) {
	f := DraftFile{SourceRun: "run.jsonl", CreatedAt: "2026-09-24T00:00:00Z", Provider: "venice",
		Model: "m", Tier: "cheap", TotalCostUSD: 0.001,
		Drafts: []Draft{{ForQuestion: "m02", Kind: DraftGotcha, Level: "model", Text: "t", Justification: "j"}}}
	path := filepath.Join(t.TempDir(), "drafts.yaml")
	if err := WriteDraftFile(path, f); err != nil {
		t.Fatal(err)
	}
	back, err := ParseDraftFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.SourceRun != f.SourceRun || len(back.Drafts) != 1 || back.Drafts[0].Text != "t" {
		t.Fatalf("round trip = %+v", back)
	}
}

func TestAuthorPromptContains(t *testing.T) {
	sem := authorSemantic()
	q, d := authorDiagnosis()
	p := AuthorPrompt(sem, q, d)
	for _, want := range []string{q.Text, d.Class, "Filter rows with WHERE on status", "gross_revenue", "kind: gotcha", "kind: synonym", "kind: metric_note", "kind: example"} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if !strings.Contains(p, "only the submitted answer text is misformatted") {
		t.Fatal("format diagnosis prompt missing the formatting guidance")
	}
	d.Class, d.Detail = ClassWrongFilter, "status excluded"
	p = AuthorPrompt(sem, q, d)
	if strings.Contains(p, "only the submitted answer text is misformatted") {
		t.Fatal("non-format diagnosis prompt should not carry the formatting guidance")
	}
}

func TestValidateDraftRejectsBenchmarkCopies(t *testing.T) {
	sem := authorSemantic()
	q, _ := authorDiagnosis()
	copies := []Draft{
		{Kind: DraftExample, Level: semantic.LevelTopic, Question: "  TEST question   m02 ", SQL: "SELECT COUNT(*) FROM users", Justification: "why"},
		{Kind: DraftExample, Level: semantic.LevelTopic, Question: "Which brands sell most?", SQL: "select brand from t group by brand  order by count(*) desc limit 5", Justification: "why"},
	}
	for i, d := range copies {
		if err := d.Validate(sem); err != nil {
			t.Fatalf("copies[%d] without benchmark: %v", i, err)
		}
		if err := d.Validate(sem, q); err == nil || !strings.Contains(err.Error(), "m02") {
			t.Fatalf("copies[%d]: err %v, want a benchmark copy error", i, err)
		}
	}
	ok := Draft{Kind: DraftExample, Level: semantic.LevelTopic, Question: "Which brands sell least?", SQL: "SELECT brand FROM t GROUP BY brand ORDER BY COUNT(*) ASC LIMIT 5", Justification: "why"}
	if err := ok.Validate(sem, q); err != nil {
		t.Fatalf("distinct example: %v", err)
	}
}

func TestAuthorOneRejectsBenchmarkCopy(t *testing.T) {
	sem := authorSemantic()
	q, d := authorDiagnosis()
	replies := []string{
		"kind: example\nlevel: topic\nquestion: \"" + q.Text + "\"\nsql: \"SELECT COUNT(*) FROM users\"\njustification: \"x\"\n",
		"kind: gotcha\nlevel: model\ntext: \"Submit bare labels.\"\njustification: \"x\"\n",
	}
	calls := 0
	var prompts []string
	call := func(ctx context.Context, purpose, prompt string) (AuthorReply, error) {
		prompts = append(prompts, prompt)
		calls++
		return AuthorReply{Text: replies[calls-1]}, nil
	}
	draft, _, err := AuthorOne(context.Background(), call, sem, q, d)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || draft.Kind != DraftGotcha || !strings.Contains(prompts[1], "copies benchmark question") {
		t.Fatalf("calls %d draft %+v", calls, draft)
	}
}
