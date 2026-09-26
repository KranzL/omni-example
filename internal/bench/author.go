package bench

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

const (
	DraftGotcha     = "gotcha"
	DraftSynonym    = "synonym"
	DraftMetricNote = "metric_note"
	DraftExample    = "example"

	MaxDraftText = 800
	MaxAttempts  = 2
)

type Draft struct {
	ForQuestion   string `yaml:"for_question"`
	Kind          string `yaml:"kind"`
	Level         string `yaml:"level"`
	Text          string `yaml:"text,omitempty"`
	Term          string `yaml:"term,omitempty"`
	RefersTo      string `yaml:"refers_to,omitempty"`
	Metric        string `yaml:"metric,omitempty"`
	Note          string `yaml:"note,omitempty"`
	Question      string `yaml:"question,omitempty"`
	SQL           string `yaml:"sql,omitempty"`
	Justification string `yaml:"justification"`
}

type DraftFile struct {
	SourceRun    string  `yaml:"source_run"`
	CreatedAt    string  `yaml:"created_at"`
	Provider     string  `yaml:"provider"`
	Model        string  `yaml:"model"`
	Tier         string  `yaml:"tier"`
	TotalCostUSD float64 `yaml:"total_cost_usd"`
	Drafts       []Draft `yaml:"drafts"`
}

type AuthorReply struct {
	Text    string
	Model   string
	Usage   llm.Usage
	CostUSD float64
}

type AuthorCall func(ctx context.Context, purpose, prompt string) (AuthorReply, error)

func AuthorPrompt(sem *semantic.Semantic, q Question, d Diagnosis) string {
	var b strings.Builder
	b.WriteString("You draft one addition to the AI context of an ecommerce analytics agent. The agent answers questions by writing SQL against six tables (users, order_items, events, inventory_items, products, distribution_centers). One benchmark question failed. Propose exactly one context addition that would have prevented this failure.\n")
	b.WriteString("\nFailure\n")
	fmt.Fprintf(&b, "question id: %s\n", d.ID)
	fmt.Fprintf(&b, "question: %s\n", q.Text)
	fmt.Fprintf(&b, "answer type: %s\n", q.AnswerType)
	fmt.Fprintf(&b, "machine classification: %s: %s\n", d.Class, d.Detail)
	if d.Class == ClassFormat {
		b.WriteString("The agent SQL already computes the right result; only the submitted answer text is misformatted. Draft a rule about answer text shape, not about SQL.\n")
	}
	fmt.Fprintf(&b, "expected answer:\n%s", indent(FormatExpected(d.Expected), "  "))
	fmt.Fprintf(&b, "ground truth SQL: %s\n", oneLine(q.SQL, 600))
	fmt.Fprintf(&b, "agent SQL: %s\n", oneLine(d.AgentSQL, 600))
	fmt.Fprintf(&b, "agent answer:\n%s", indent(d.AgentAnswer+"\n", "  "))
	b.WriteString("\nExisting context (do not duplicate any of this)\n")
	b.WriteString("Gotchas:\n")
	for i, g := range sem.Gotchas {
		fmt.Fprintf(&b, "%d. %s\n", i+1, g)
	}
	b.WriteString("Conventions:\n")
	for i, c := range sem.Conventions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, c)
	}
	b.WriteString("Synonyms:\n")
	for _, s := range sem.Synonyms {
		fmt.Fprintf(&b, "- %s means %s\n", s.Term, s.RefersTo)
	}
	b.WriteString("Metrics:\n")
	for _, m := range sem.Metrics {
		fmt.Fprintf(&b, "- %s: %s\n", m.Name, m.Description)
	}
	b.WriteString("Worked example questions:\n")
	for _, e := range sem.Examples {
		fmt.Fprintf(&b, "- %s\n", e.Question)
	}
	b.WriteString("\nReply with exactly one YAML mapping and no other text. Use one of these four shapes.\n")
	b.WriteString("A gotcha, a one-sentence trap warning:\n")
	b.WriteString("  for_question: " + d.ID + "\n")
	b.WriteString("  kind: gotcha\n")
	b.WriteString("  level: model\n")
	b.WriteString("  text: \"the warning sentence\"\n")
	b.WriteString("  justification: \"one sentence saying why this fixes the failure\"\n")
	b.WriteString("A synonym, a question word and the table or column it means:\n")
	b.WriteString("  for_question: " + d.ID + "\n")
	b.WriteString("  kind: synonym\n")
	b.WriteString("  level: field\n")
	b.WriteString("  term: \"the question word\"\n")
	b.WriteString("  refers_to: table or table.column\n")
	b.WriteString("  justification: \"one sentence saying why this fixes the failure\"\n")
	b.WriteString("A metric note, one sentence appended to a metric definition:\n")
	b.WriteString("  for_question: " + d.ID + "\n")
	b.WriteString("  kind: metric_note\n")
	b.WriteString("  level: topic\n")
	b.WriteString("  metric: the metric name\n")
	b.WriteString("  note: \"the sentence\"\n")
	b.WriteString("  justification: \"one sentence saying why this fixes the failure\"\n")
	b.WriteString("A worked example, a short question with correct SQL:\n")
	b.WriteString("  for_question: " + d.ID + "\n")
	b.WriteString("  kind: example\n")
	b.WriteString("  level: topic\n")
	b.WriteString("  question: \"the example question\"\n")
	b.WriteString("  sql: \"the correct read-only SQL on one line\"\n")
	b.WriteString("  justification: \"one sentence saying why this fixes the failure\"\n")
	b.WriteString("Always wrap text, term, note, question, sql, and justification values in double quotes, because unquoted colons break YAML. ")
	b.WriteString("Levels: model holds global rules, topic holds dataset guidance, field holds table and column wording. ")
	b.WriteString("Keep text and note under 800 characters. The example SQL must be one read-only SELECT or WITH statement. No prose outside the YAML mapping.")
	return b.String()
}

func ParseDraftReply(text string) (Draft, error) {
	body := strings.TrimSpace(text)
	if start := strings.Index(body, "```"); start >= 0 {
		rest := body[start+3:]
		if nl := strings.Index(rest, "\n"); nl >= 0 {
			rest = rest[nl+1:]
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			body = strings.TrimSpace(rest[:end])
		} else {
			body = strings.TrimSpace(rest)
		}
	}
	var d Draft
	dec := yaml.NewDecoder(strings.NewReader(body))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return Draft{}, fmt.Errorf("draft is not a valid mapping: %w", err)
	}
	return d, nil
}

func (d Draft) Validate(sem *semantic.Semantic, benchmark ...Question) error {
	switch d.Kind {
	case DraftGotcha, DraftSynonym, DraftMetricNote, DraftExample:
	default:
		return fmt.Errorf("unknown kind %q, want gotcha, synonym, metric_note or example", d.Kind)
	}
	switch d.Level {
	case semantic.LevelModel, semantic.LevelTopic, semantic.LevelField:
	default:
		return fmt.Errorf("unknown level %q, want model, topic or field", d.Level)
	}
	if strings.TrimSpace(d.Justification) == "" {
		return fmt.Errorf("justification is empty")
	}
	switch d.Kind {
	case DraftGotcha:
		return validateGotcha(sem, d.Text)
	case DraftSynonym:
		return validateSynonym(sem, d.Term, d.RefersTo)
	case DraftMetricNote:
		return validateMetricNote(sem, d.Metric, d.Note)
	case DraftExample:
		return validateExample(sem, d.Question, d.SQL, benchmark)
	}
	return nil
}

func normalizeText(s string) string {
	return spacePattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), " ")
}

func validateGotcha(sem *semantic.Semantic, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("gotcha text is empty")
	}
	if len(text) > MaxDraftText {
		return fmt.Errorf("gotcha text has %d characters, limit %d", len(text), MaxDraftText)
	}
	norm := normalizeText(text)
	for _, g := range sem.Gotchas {
		if normalizeText(g) == norm {
			return fmt.Errorf("gotcha duplicates an existing gotcha")
		}
	}
	for _, c := range sem.Conventions {
		if normalizeText(c) == norm {
			return fmt.Errorf("gotcha duplicates an existing convention")
		}
	}
	return nil
}

func validateSynonym(sem *semantic.Semantic, term, refersTo string) error {
	if strings.TrimSpace(term) == "" {
		return fmt.Errorf("synonym term is empty")
	}
	if strings.TrimSpace(refersTo) == "" {
		return fmt.Errorf("synonym refers_to is empty")
	}
	known := map[string]map[string]bool{}
	for _, t := range sem.Tables {
		cols := map[string]bool{}
		for _, c := range t.Columns {
			cols[c.Name] = true
		}
		known[t.Name] = cols
	}
	if table, col, ok := strings.Cut(refersTo, "."); ok {
		cols, found := known[table]
		if !found {
			return fmt.Errorf("synonym refers to unknown table %q", table)
		}
		if !cols[col] {
			return fmt.Errorf("synonym refers to unknown column %q", refersTo)
		}
	} else if _, found := known[refersTo]; !found {
		return fmt.Errorf("synonym refers to unknown table %q", refersTo)
	}
	for _, s := range sem.Synonyms {
		if strings.EqualFold(s.Term, term) {
			return fmt.Errorf("synonym term %q already exists", term)
		}
	}
	return nil
}

func validateMetricNote(sem *semantic.Semantic, metric, note string) error {
	if strings.TrimSpace(note) == "" {
		return fmt.Errorf("metric note is empty")
	}
	if len(note) > MaxDraftText {
		return fmt.Errorf("metric note has %d characters, limit %d", len(note), MaxDraftText)
	}
	for _, m := range sem.Metrics {
		if m.Name == metric {
			if strings.Contains(normalizeText(m.Description), normalizeText(note)) {
				return fmt.Errorf("metric note duplicates text already in %q", metric)
			}
			return nil
		}
	}
	return fmt.Errorf("unknown metric %q", metric)
}

func validateExample(sem *semantic.Semantic, question, sql string, benchmark []Question) error {
	if strings.TrimSpace(question) == "" {
		return fmt.Errorf("example question is empty")
	}
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("example SQL is empty")
	}
	if err := db.Validate(sql); err != nil {
		return fmt.Errorf("example SQL is not a read-only query: %w", err)
	}
	norm := normalizeText(question)
	for _, e := range sem.Examples {
		if normalizeText(e.Question) == norm {
			return fmt.Errorf("example question duplicates an existing example")
		}
	}
	normSQL := normalizeSQL(sql)
	for _, q := range benchmark {
		if normalizeText(q.Text) == norm {
			return fmt.Errorf("example question copies benchmark question %s", q.ID)
		}
		if strings.TrimSpace(q.SQL) != "" && normalizeSQL(q.SQL) == normSQL {
			return fmt.Errorf("example SQL copies the ground truth SQL of benchmark question %s", q.ID)
		}
	}
	return nil
}

func AuthorOne(ctx context.Context, call AuthorCall, sem *semantic.Semantic, q Question, d Diagnosis) (Draft, []AuthorReply, error) {
	prompt := AuthorPrompt(sem, q, d)
	var replies []AuthorReply
	var lastErr error
	lastReply := ""
	for attempt := 0; attempt < MaxAttempts; attempt++ {
		reply, err := call(ctx, "context-author-"+d.ID, prompt)
		if err != nil {
			return Draft{}, replies, fmt.Errorf("question %s: %w", d.ID, err)
		}
		replies = append(replies, reply)
		lastReply = reply.Text
		draft, perr := ParseDraftReply(reply.Text)
		if perr != nil {
			lastErr = perr
		} else {
			draft.ForQuestion = d.ID
			lastErr = draft.Validate(sem, q)
			if lastErr == nil {
				return draft, replies, nil
			}
		}
		prompt = prompt + "\n\nYour previous reply failed validation: " + lastErr.Error() + " Reply again with exactly one YAML mapping and no other text."
	}
	return Draft{}, replies, fmt.Errorf("question %s: %v; reply was %q", d.ID, lastErr, oneLine(lastReply, 300))
}

func WriteDraftFile(path string, f DraftFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func ParseDraftFile(path string) (DraftFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DraftFile{}, err
	}
	var f DraftFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return DraftFile{}, err
	}
	return f, nil
}

func (d Draft) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "- for %s: %s (%s level)\n", d.ForQuestion, d.Kind, d.Level)
	switch d.Kind {
	case DraftGotcha:
		fmt.Fprintf(&b, "  text: %s\n", d.Text)
	case DraftSynonym:
		fmt.Fprintf(&b, "  %s means %s\n", d.Term, d.RefersTo)
	case DraftMetricNote:
		fmt.Fprintf(&b, "  %s: %s\n", d.Metric, d.Note)
	case DraftExample:
		fmt.Fprintf(&b, "  Q: %s\n", d.Question)
		fmt.Fprintf(&b, "  SQL: %s\n", oneLine(d.SQL, 300))
	}
	fmt.Fprintf(&b, "  justification: %s\n", d.Justification)
	return b.String()
}
