package semantic

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/config"
)

const ecommPath = "../../semantic/ecomm.yaml"

func loadEcomm(t *testing.T) *Semantic {
	t.Helper()
	s, err := Load(ecommPath)
	if err != nil {
		t.Fatalf("Load(%s) = %v", ecommPath, err)
	}
	return s
}

func TestLoadEcomm(t *testing.T) {
	s := loadEcomm(t)
	if s.Schema != "public" {
		t.Fatalf("Schema = %q, want public", s.Schema)
	}
	if len(s.Tables) != 6 {
		t.Fatalf("len(Tables) = %d, want 6", len(s.Tables))
	}
	if len(s.Metrics) != 11 {
		t.Fatalf("len(Metrics) = %d, want 11", len(s.Metrics))
	}
	if len(s.Examples) < 8 {
		t.Fatalf("len(Examples) = %d, want at least 8", len(s.Examples))
	}
	if len(s.Paths) != 6 {
		t.Fatalf("len(Paths) = %d, want 6", len(s.Paths))
	}
	if len(s.Synonyms) != 4 {
		t.Fatalf("len(Synonyms) = %d, want 4", len(s.Synonyms))
	}
	if len(s.Conventions) == 0 {
		t.Fatal("no conventions loaded")
	}
	cols := 0
	for _, tbl := range s.Tables {
		cols += len(tbl.Columns)
	}
	if cols != 68 {
		t.Fatalf("total columns = %d, want 68", cols)
	}
}

func TestRenderDeterministic(t *testing.T) {
	first := loadEcomm(t).Render()
	second := loadEcomm(t).Render()
	if first != second {
		t.Fatal("two renders differ")
	}
	a := sha256.Sum256([]byte(first))
	b := sha256.Sum256([]byte(second))
	if a != b {
		t.Fatalf("sha256 differs: %x vs %x", a, b)
	}
	t.Logf("render sha256: %x, bytes: %d", a, len(first))
}

func TestRenderSortsKeys(t *testing.T) {
	data := `
schema: public
dataset: test
tables:
  - name: b_table
    purpose: second
    grain: row
    primary_key: alpha
    columns:
      - name: zeta
        type: integer
        description: zed
      - name: alpha
        type: integer
        description: ay
  - name: a_table
    purpose: first
    grain: row
    primary_key: id
    columns:
      - name: id
        type: integer
        description: key
metrics:
  - name: zulu
    description: z
    status_filter: none
    sql: SELECT 1
  - name: alpha
    description: a
    status_filter: none
    sql: SELECT 1
paths:
  - name: zulu_path
    description: zed path
    grain: one zed row
    steps:
      - from: a_table
        join: b_table
        on: b_table.alpha = a_table.id
  - name: alpha_path
    description: ay path
    grain: one ay row
    steps:
      - from: b_table
        join: a_table
        on: a_table.id = b_table.alpha
synonyms:
  - term: zebra
    refers_to: b_table
  - term: apple
    refers_to: a_table.id
conventions:
  - one
gotchas:
  - watch out
examples:
  - question: Zebra?
    sql: SELECT 1
  - question: Apple?
    sql: SELECT 1
`
	s, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	out := s.Render()
	pairs := [][2]string{
		{"## a_table", "## b_table"},
		{"- alpha (integer)", "- zeta (integer)"},
		{"## alpha", "## zulu"},
		{"## alpha_path", "## zulu_path"},
		{"apple means a_table.id", "zebra means b_table"},
		{"Q: Apple?", "Q: Zebra?"},
	}
	for _, p := range pairs {
		i, j := strings.Index(out, p[0]), strings.Index(out, p[1])
		if i < 0 || j < 0 || i > j {
			t.Fatalf("render order wrong for %q before %q", p[0], p[1])
		}
	}
}

func TestRenderGotchas(t *testing.T) {
	s := loadEcomm(t)
	if len(s.Gotchas) == 0 {
		t.Fatal("no gotchas loaded")
	}
	out := s.Render()
	if !strings.Contains(out, "Gotchas (6)") {
		t.Fatalf("render missing gotchas section header")
	}
	wants := []string{
		"filter rows with WHERE",
		"MIN(created_at) over its items",
		"relative to the user's created_at",
		"never append counts or metrics as suffix text",
		"without appending metrics like revenue or counts",
		"Never use key=value inline notation",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("render missing gotcha %q", w)
		}
	}
}

func TestRenderPaths(t *testing.T) {
	s := loadEcomm(t)
	if len(s.Paths) == 0 {
		t.Fatal("no paths loaded")
	}
	if len(s.Synonyms) == 0 {
		t.Fatal("no synonyms loaded")
	}
	out := s.Render()
	if !strings.Contains(out, "Join paths (6)") {
		t.Fatalf("render missing join paths section header")
	}
	if !strings.Contains(out, "Synonyms (4)") {
		t.Fatalf("render missing synonyms section header")
	}
	wants := []string{
		"## order_item_to_warehouse",
		"ON distribution_centers.id = inventory_items.product_distribution_center_id",
		"ON users.id = events.user_id",
		"ON order_items.user_id = users.id",
		"Grain: One row per order item.",
		"warehouse means distribution_centers",
		"customer means users",
		"line item means order_items",
		"visit means events.session_id",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("render missing %q", w)
		}
	}
	pathsIdx := strings.Index(out, "Join paths (6)")
	metricsIdx := strings.Index(out, "Metrics (")
	if pathsIdx < 0 || metricsIdx < 0 || pathsIdx > metricsIdx {
		t.Fatalf("join paths section is not before metrics")
	}
}

func TestRenderContainsContract(t *testing.T) {
	out := loadEcomm(t).Render()
	wants := []string{
		"Schema: public",
		"## gross_revenue",
		"## net_revenue",
		"## conversion_rate",
		"Allowed values: Cancelled, Complete, Processing, Returned, Shipped.",
		"Allowed values: Female, Male.",
		"Allowed values: Display, Email, Facebook, Organic, Search.",
		"Allowed values: Adwords, Email, Facebook, Organic, YouTube.",
		"user_id references users.id.",
		"inventory_item_id references inventory_items.id.",
		"product_id references products.id.",
		"distribution_center_id references distribution_centers.id.",
		"An order is the set of order_items rows sharing one order_id.",
		"Measure sales from order_items, never from inventory_items.sold_at.",
		"Q: What was gross revenue by month in 2024?",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Fatalf("render missing %q", w)
		}
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Semantic)
	}{
		{"empty schema", func(s *Semantic) { s.Schema = "" }},
		{"bad primary key", func(s *Semantic) { s.Tables[0].PrimaryKey = "nope" }},
		{"bad fk column", func(s *Semantic) { s.Tables[3].ForeignKeys[0].Column = "nope" }},
		{"bad fk table", func(s *Semantic) { s.Tables[3].ForeignKeys[0].References = "nope.id" }},
		{"bad fk refcol", func(s *Semantic) { s.Tables[3].ForeignKeys[0].References = "users.nope" }},
		{"malformed fk", func(s *Semantic) { s.Tables[3].ForeignKeys[0].References = "users" }},
		{"bad enum table", func(s *Semantic) { s.Enums[0].Column = "nope.browser" }},
		{"bad enum column", func(s *Semantic) { s.Enums[0].Column = "events.nope" }},
		{"malformed enum", func(s *Semantic) { s.Enums[0].Column = "events" }},
		{"empty enum values", func(s *Semantic) { s.Enums[0].Values = nil }},
		{"empty metric sql", func(s *Semantic) { s.Metrics[0].SQL = "" }},
		{"empty status filter", func(s *Semantic) { s.Metrics[0].StatusFilter = " " }},
		{"no paths", func(s *Semantic) { s.Paths = nil }},
		{"empty path name", func(s *Semantic) { s.Paths[0].Name = " " }},
		{"duplicate path name", func(s *Semantic) { s.Paths[1].Name = s.Paths[0].Name }},
		{"empty path grain", func(s *Semantic) { s.Paths[0].Grain = "" }},
		{"path with no steps", func(s *Semantic) { s.Paths[0].Steps = nil }},
		{"bad path step table", func(s *Semantic) { s.Paths[0].Steps[0].Join = "nope" }},
		{"bad path on table", func(s *Semantic) { s.Paths[0].Steps[0].On = "nope.id = order_items.inventory_item_id" }},
		{"bad path on column", func(s *Semantic) { s.Paths[0].Steps[0].On = "order_items.nope = inventory_items.id" }},
		{"malformed path on", func(s *Semantic) { s.Paths[0].Steps[0].On = "order_items" }},
		{"no synonyms", func(s *Semantic) { s.Synonyms = nil }},
		{"empty synonym term", func(s *Semantic) { s.Synonyms[0].Term = "" }},
		{"duplicate synonym term", func(s *Semantic) { s.Synonyms[1].Term = s.Synonyms[0].Term }},
		{"bad synonym table", func(s *Semantic) { s.Synonyms[0].RefersTo = "nope" }},
		{"bad synonym column", func(s *Semantic) { s.Synonyms[0].RefersTo = "users.nope" }},
		{"no conventions", func(s *Semantic) { s.Conventions = nil }},
		{"no gotchas", func(s *Semantic) { s.Gotchas = nil }},
		{"no examples", func(s *Semantic) { s.Examples = nil }},
	}
	for _, c := range cases {
		data, err := os.ReadFile(ecommPath)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		c.mutate(s)
		if err := s.Validate(); err == nil {
			t.Fatalf("%s: Validate() = nil, want error", c.name)
		}
	}
}

func TestDefaultPath(t *testing.T) {
	t.Chdir("../..")
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("semantic", FileName)
	rel := got
	if filepath.IsAbs(got) {
		cwd, _ := os.Getwd()
		rel, _ = filepath.Rel(cwd, got)
	}
	if rel != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
	if _, err := Load(got); err != nil {
		t.Fatalf("Load(DefaultPath()) = %v", err)
	}
}

func TestApproxTokens(t *testing.T) {
	if got := ApproxTokens(""); got != 0 {
		t.Fatalf("ApproxTokens(\"\") = %d, want 0", got)
	}
	if got := ApproxTokens("abcd"); got != 1 {
		t.Fatalf("ApproxTokens(4 chars) = %d, want 1", got)
	}
	out := loadEcomm(t).Render()
	got := ApproxTokens(out)
	if got < 1000 {
		t.Fatalf("ApproxTokens(render) = %d, want at least 1000", got)
	}
	t.Logf("approx tokens: %d for %d bytes", got, len(out))
}

func TestParseLevels(t *testing.T) {
	cases := []struct {
		spec string
		want Levels
	}{
		{"", AllLevels()},
		{"all", AllLevels()},
		{"ALL", AllLevels()},
		{"model", Levels{Model: true}},
		{"topic", Levels{Topic: true}},
		{"field", Levels{Field: true}},
		{"model,topic", Levels{Model: true, Topic: true}},
		{"field,model", Levels{Model: true, Field: true}},
		{"model,topic,field", AllLevels()},
		{" model , topic ", Levels{Model: true, Topic: true}},
		{"model,model", Levels{Model: true}},
	}
	for _, c := range cases {
		got, err := ParseLevels(c.spec)
		if err != nil {
			t.Errorf("ParseLevels(%q) = %v, want %v", c.spec, err, c.want)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevels(%q) = %v, want %v", c.spec, got, c.want)
		}
	}
	for _, spec := range []string{"foo", "model,foo", "model,", "none", "topics"} {
		if _, err := ParseLevels(spec); err == nil {
			t.Errorf("ParseLevels(%q) = nil error, want error", spec)
		}
	}
}

func TestLevelsString(t *testing.T) {
	cases := map[Levels]string{
		AllLevels():                      "all",
		Levels{Model: true}:              "model",
		Levels{Model: true, Topic: true}: "model,topic",
		Levels{Field: true, Model: true}: "model,field",
	}
	for levels, want := range cases {
		if got := levels.String(); got != want {
			t.Errorf("%v.String() = %q, want %q", levels, got, want)
		}
	}
	if got := (Levels{Model: true, Topic: true}).Slug(); got != "model-topic" {
		t.Errorf("Slug() = %q, want model-topic", got)
	}
}

func TestRenderLevelsDeterministic(t *testing.T) {
	configs := map[string]Levels{
		"all":               AllLevels(),
		"model":             {Model: true},
		"model-topic":       {Model: true, Topic: true},
		"model-topic-field": {Model: true, Topic: true, Field: true},
	}
	for name, levels := range configs {
		first := loadEcomm(t).RenderLevels(levels)
		second := loadEcomm(t).RenderLevels(levels)
		if first != second {
			t.Errorf("%s: two renders differ", name)
		}
		sum := sha256.Sum256([]byte(first))
		t.Logf("%s sha256: %x, bytes: %d", name, sum, len(first))
	}
	if got := loadEcomm(t).Render(); got != loadEcomm(t).RenderLevels(AllLevels()) {
		t.Error("Render() differs from RenderLevels(all)")
	}
	explicit, err := ParseLevels("model,topic,field")
	if err != nil {
		t.Fatal(err)
	}
	if got := loadEcomm(t).RenderLevels(explicit); got != loadEcomm(t).Render() {
		t.Error("RenderLevels(model,topic,field) differs from Render()")
	}
}

func TestRenderLevelsSections(t *testing.T) {
	s := loadEcomm(t)
	model := s.RenderLevels(Levels{Model: true})
	for _, want := range []string{"Tables (6)", "Conventions (10)", "Gotchas (6)", "- id (integer)\n"} {
		if !strings.Contains(model, want) {
			t.Errorf("model-only render missing %q", want)
		}
	}
	for _, missing := range []string{"Join paths (", "Synonyms (", "Metrics (", "Worked examples (", "Allowed values:", "Warehouse identifier", "means distribution_centers"} {
		if strings.Contains(model, missing) {
			t.Errorf("model-only render contains %q", missing)
		}
	}
	modelTopic := s.RenderLevels(Levels{Model: true, Topic: true})
	for _, want := range []string{"Join paths (6)", "Metrics (11)", "Worked examples (14)", "Conventions (10)", "Gotchas (6)"} {
		if !strings.Contains(modelTopic, want) {
			t.Errorf("model-plus-topic render missing %q", want)
		}
	}
	for _, missing := range []string{"Synonyms (", "Allowed values:", "Warehouse identifier"} {
		if strings.Contains(modelTopic, missing) {
			t.Errorf("model-plus-topic render contains %q", missing)
		}
	}
	all := s.Render()
	for _, want := range []string{"Join paths (6)", "Synonyms (4)", "Metrics (11)", "Conventions (10)", "Gotchas (6)", "Worked examples (14)", "Allowed values:", "warehouse means distribution_centers"} {
		if !strings.Contains(all, want) {
			t.Errorf("all-levels render missing %q", want)
		}
	}
}

func TestSectionTokenCounts(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is absent")
	}
	s := loadEcomm(t)
	ctx := context.Background()
	count := func(text string) int64 {
		t.Helper()
		n, err := CountTokens(ctx, text, key, os.Getenv(config.AnthropicWorkspace))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	sections := []struct {
		name string
		text string
	}{
		{"base", s.renderTables(false)},
		{"paths", s.renderPaths()},
		{"synonyms", s.renderSynonyms()},
		{"metrics", s.renderMetrics()},
		{"conventions", s.renderConventions()},
		{"gotchas", s.renderGotchas()},
		{"examples", s.renderExamples()},
	}
	for _, sec := range sections {
		if sec.text == "" {
			t.Errorf("section %s renders empty", sec.name)
		}
		t.Logf("section %-12s tokens=%d", sec.name, count(sec.text))
	}
	configs := []struct {
		name   string
		levels Levels
	}{
		{"all", AllLevels()},
		{"model", Levels{Model: true}},
		{"model-topic", Levels{Model: true, Topic: true}},
		{"model-topic-field", Levels{Model: true, Topic: true, Field: true}},
	}
	counts := map[string]int64{}
	for _, c := range configs {
		n := count(s.RenderLevels(c.levels))
		counts[c.name] = n
		t.Logf("config %-18s tokens=%d", c.name, n)
	}
	if counts["all"] != counts["model-topic-field"] {
		t.Errorf("all=%d differs from model-topic-field=%d", counts["all"], counts["model-topic-field"])
	}
	if !(counts["model"] < counts["model-topic"] && counts["model-topic"] < counts["all"]) {
		t.Errorf("counts do not grow with levels: %v", counts)
	}
	modelOnly := s.RenderLevels(Levels{Model: true})
	if agent.InstructionsFor(modelOnly) == agent.Instructions {
		t.Fatal("model-only prefix needs padding but InstructionsFor returned the base text")
	}
	if got := agent.InstructionsFor(s.Render()); got != agent.Instructions {
		t.Error("all-levels render pads the instructions but must leave them unchanged")
	}
	t.Logf("instructions base tokens=%d", count(agent.Instructions))
	for _, c := range configs {
		render := s.RenderLevels(c.levels)
		instructions := agent.InstructionsFor(render)
		padded := "no"
		if instructions != agent.Instructions {
			padded = "yes"
		}
		prefix := count(instructions + "\n" + render)
		t.Logf("prefix %-18s tokens=%d padded=%s", c.name, prefix, padded)
		if prefix < 4096 {
			t.Errorf("prefix %s = %d tokens, want at least 4096", c.name, prefix)
		}
	}
}

func TestCountTokensIntegration(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY is absent")
	}
	out := loadEcomm(t).Render()
	n, err := CountTokens(context.Background(), out, key, os.Getenv(config.AnthropicWorkspace))
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("count_tokens: %d\n", n)
	if n < 4096 {
		t.Fatalf("CountTokens(render) = %d, want at least 4096", n)
	}
}

func TestCountTokensSendsWorkspaceHeader(t *testing.T) {
	var gotWorkspace, gotKey, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotWorkspace = r.Header.Get("anthropic-workspace-id")
		gotKey = r.Header.Get("X-Api-Key")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"input_tokens":42}`))
	}))
	defer srv.Close()
	n, err := CountTokens(context.Background(), "hello", "test-key", "wrkspc_1", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	if n != 42 || gotWorkspace != "wrkspc_1" || gotKey != "test-key" || !strings.HasSuffix(gotPath, "/count_tokens") {
		t.Fatalf("n=%d workspace=%q key=%q path=%q", n, gotWorkspace, gotKey, gotPath)
	}
	gotWorkspace = "unset"
	if _, err := CountTokens(context.Background(), "hello", "test-key", "", option.WithBaseURL(srv.URL), option.WithMaxRetries(0)); err != nil {
		t.Fatal(err)
	}
	if gotWorkspace != "" {
		t.Fatalf("empty workspace sent header %q", gotWorkspace)
	}
}
