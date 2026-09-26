package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"gopkg.in/yaml.v3"

	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

func loadSemantic(t *testing.T) *semantic.Semantic {
	t.Helper()
	sem, err := semantic.Load(filepath.Join("..", "..", "semantic", semantic.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return sem
}

func TestBaselines(t *testing.T) {
	for name, want := range map[string]llm.Tier{
		NameAlwaysCheap: llm.TierCheap,
		NameAlwaysMid:   llm.TierMid,
		NameAlwaysTop:   llm.TierTop,
	} {
		f, ok := Baseline(name)
		if !ok || f.Name() != name {
			t.Fatalf("%s: baseline %+v %v", name, f, ok)
		}
		d, err := f.Route(context.Background(), "anything")
		if err != nil || d.Tier != want || d.Cost != 0 || d.Label != LabelFixed {
			t.Errorf("%s: decision %+v %v", name, d, err)
		}
	}
	if _, ok := Baseline(NameHeuristic); ok {
		t.Error("heuristic is not a baseline")
	}
	for _, n := range Names() {
		if err := ValidName(n); err != nil {
			t.Error(err)
		}
	}
	if ValidName("always-nano") == nil {
		t.Error("unknown router: want error")
	}
}

func TestTierForLabel(t *testing.T) {
	for label, want := range map[string]llm.Tier{LabelEasy: llm.TierCheap, LabelModerate: llm.TierMid, LabelHard: llm.TierTop} {
		if got, err := TierForLabel(label); err != nil || got != want {
			t.Errorf("%s = %s %v", label, got, err)
		}
	}
	if _, err := TierForLabel("expert"); err == nil {
		t.Error("expert: want error")
	}
}

func TestCanonicalLabelBirdAliases(t *testing.T) {
	for label, want := range map[string]string{"simple": LabelEasy, "moderate": LabelModerate, "challenging": LabelHard} {
		got, err := CanonicalLabel(label)
		if err != nil || got != want {
			t.Errorf("%s = %q %v, want %q", label, got, err, want)
		}
	}
	if got, err := TierForLabel("simple"); err != nil || got != llm.TierCheap {
		t.Errorf("simple tier = %s %v", got, err)
	}
	if got, err := TierForLabel("challenging"); err != nil || got != llm.TierTop {
		t.Errorf("challenging tier = %s %v", got, err)
	}
}

func TestSchemaTerms(t *testing.T) {
	terms := SchemaTerms(loadSemantic(t))
	have := map[string]bool{}
	for _, term := range terms {
		have[term] = true
	}
	for _, want := range []string{"order items", "order item", "users", "user", "traffic source", "gross revenue", "email", "brand"} {
		if !have[want] {
			t.Errorf("missing term %q", want)
		}
	}
	if have["id"] || have["name"] {
		t.Error("generic columns id and name must be excluded")
	}
	for i := 1; i < len(terms); i++ {
		if len(terms[i]) > len(terms[i-1]) {
			t.Fatalf("terms not sorted longest first at %d: %q after %q", i, terms[i], terms[i-1])
		}
	}
}

func TestHeuristicFeatures(t *testing.T) {
	h := NewHeuristic(loadSemantic(t), DefaultHeuristicParams())
	f := h.Features("What was the median days from first to second order for users acquired through Facebook, by traffic source, in 2024?")
	want := Features{Words: 20, SchemaMentions: 2, Aggregation: 1, Time: 2, Ranking: 0, Reasoning: 0, Qualifier: 2}
	if f != want {
		t.Errorf("features %+v, want %+v", f, want)
	}
	f = h.Features("Why did gross revenue drop? Predict next month and recommend a fix.")
	if f.Reasoning != 3 || f.Time != 1 || f.SchemaMentions != 1 {
		t.Errorf("reasoning features %+v", f)
	}
	f = h.Features("How many order items are there?")
	if f.SchemaMentions != 1 {
		t.Errorf("order items must count once, not also as order item: %+v", f)
	}
}

func TestHeuristicScoreAndLabel(t *testing.T) {
	p := HeuristicParams{PerWord: 1, SchemaMention: 2, Aggregation: 3, Time: 4, Ranking: 5, Reasoning: 6, Qualifier: 7, MidThreshold: 10, TopThreshold: 20}
	f := Features{Words: 1, SchemaMentions: 1, Aggregation: 1, Time: 1, Ranking: 1, Reasoning: 1, Qualifier: 1}
	if got := p.Score(f); got != 28 {
		t.Errorf("score %v, want 28", got)
	}
	for score, want := range map[float64]string{0: LabelEasy, 9.99: LabelEasy, 10: LabelModerate, 19.99: LabelModerate, 20: LabelHard, 100: LabelHard} {
		if got := p.Label(score); got != want {
			t.Errorf("label(%v) = %s, want %s", score, got, want)
		}
	}
}

func TestHeuristicRoutes(t *testing.T) {
	h := NewHeuristic(loadSemantic(t), DefaultHeuristicParams())
	cases := map[string]llm.Tier{
		"What is the email address of the user with id 42?":                                                                   llm.TierCheap,
		"How many products are in the Accessories category?":                                                                  llm.TierCheap,
		"What was the average sale price per product department in 2023, and which department had the highest?":               llm.TierMid,
		"What share of users acquired in each month of 2023 placed a second order within 60 days after their first order?":    llm.TierTop,
		"Why did repeat purchases fall in 2024, and what should we optimize to predict and recommend better retention steps?": llm.TierTop,
	}
	for q, want := range cases {
		d, err := h.Route(context.Background(), q)
		if err != nil {
			t.Fatal(err)
		}
		if d.Tier != want {
			t.Errorf("%q routed %s (%s), want %s", q, d.Tier, d.Reason, want)
		}
		if d.Cost != 0 || len(d.CallRecords) != 0 || d.Reason == "" {
			t.Errorf("heuristic decision %+v", d)
		}
	}
}

func TestHeuristicV2FixesDoubleAggregationMiss(t *testing.T) {
	sem := loadSemantic(t)
	question := "What was the 90th percentile of per-user 2024 gross spend, summing each user's non-cancelled items first?"
	v1 := NewHeuristic(sem, DefaultHeuristicParams())
	d, err := v1.Route(context.Background(), question)
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != llm.TierMid {
		t.Fatalf("default params routed %q to %s, want mid (the known miss)", question, d.Tier)
	}
	v2 := NewNamedHeuristic(NameHeuristicV2, sem, HeuristicV2Params())
	if v2.Name() != NameHeuristicV2 {
		t.Errorf("name = %s, want %s", v2.Name(), NameHeuristicV2)
	}
	d, err = v2.Route(context.Background(), question)
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != llm.TierTop {
		t.Errorf("v2 params routed %q to %s, want top", question, d.Tier)
	}
	single := "What was the total revenue in 2024?"
	d1, err := v1.Route(context.Background(), single)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := v2.Route(context.Background(), single)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Tier != d2.Tier {
		t.Errorf("a single aggregation mention should not change tier: v1=%s v2=%s", d1.Tier, d2.Tier)
	}
}

type stubProvider struct {
	msg  *anthropic.Message
	err  error
	req  llm.Request
	cost float64
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	s.req = req
	rec := llm.CallRecord{Model: llm.ModelHaiku45, Tier: req.Tier, Purpose: req.Purpose, CostUSD: s.cost, LatencyMS: 5}
	trace.Append(rec)
	return s.msg, rec, s.err
}

func toolMessage(t *testing.T, name string, input any) *anthropic.Message {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","stop_reason":"tool_use","content":[{"type":"tool_use","id":"tu_1","name":%q,"input":%s}],"usage":{"input_tokens":1,"output_tokens":1}}`, name, raw)
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatal(err)
	}
	return &msg
}

func TestClassifierRoutesFromToolCall(t *testing.T) {
	stub := &stubProvider{msg: toolMessage(t, ToolClassify, map[string]string{"label": "Moderate", "reason": " one group by "}), cost: 0.0007}
	c := NewClassifier(stub, "SEMANTIC")
	d, err := c.Route(context.Background(), "What was revenue by country?")
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != llm.TierMid || d.Label != LabelModerate || d.Reason != "one group by" {
		t.Errorf("decision %+v", d)
	}
	if d.Cost != 0.0007 || len(d.CallRecords) != 1 || d.CallRecords[0].Purpose != "route-classifier" {
		t.Errorf("overhead not recorded %+v", d)
	}
	if stub.req.Tier != llm.TierCheap || len(stub.req.Tools) != 1 {
		t.Errorf("request %+v", stub.req)
	}
	sys := stub.req.System
	if len(sys) != 2 || sys[1].Text != "SEMANTIC" || sys[1].CacheControl.Type == "" || sys[0].CacheControl.Type != "" {
		t.Errorf("system blocks must end with the cached semantic layer: %+v", sys)
	}
}

func TestClassifierErrors(t *testing.T) {
	var text anthropic.Message
	if err := json.Unmarshal([]byte(`{"id":"m","type":"message","role":"assistant","model":"claude-haiku-4-5","stop_reason":"end_turn","content":[{"type":"text","text":"hard"}],"usage":{"input_tokens":1,"output_tokens":1}}`), &text); err != nil {
		t.Fatal(err)
	}
	unparseable := map[string]*stubProvider{
		"no tool call": {msg: &text, cost: 0.001},
		"bad label":    {msg: toolMessage(t, ToolClassify, map[string]string{"label": "expert", "reason": "x"}), cost: 0.001},
		"wrong tool":   {msg: toolMessage(t, "other", map[string]string{"label": "easy", "reason": "x"}), cost: 0.001},
	}
	for name, stub := range unparseable {
		d, err := NewClassifier(stub, "S").Route(context.Background(), "q")
		if err != nil {
			t.Errorf("%s: an unparseable reply must fall back, got %v", name, err)
		}
		if d.Tier != llm.TierMid || d.Label != LabelModerate || !strings.Contains(d.Reason, "unparseable") {
			t.Errorf("%s: decision %+v, want mid fallback", name, d)
		}
		if d.Cost != 2*stub.cost || len(d.CallRecords) != 2 {
			t.Errorf("%s: the retry cost must be kept %+v", name, d)
		}
	}
	stub := &stubProvider{err: errors.New("overloaded")}
	d, err := NewClassifier(stub, "S").Route(context.Background(), "q")
	if err == nil {
		t.Errorf("api error: want error, got %+v", d)
	}
	if len(d.CallRecords) != 2 {
		t.Errorf("api error: records %+v", d.CallRecords)
	}
}

func TestClassifierExamplesAreNotBenchmarkQuestions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "bench", "questions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var qs []struct {
		Question string `yaml:"question"`
	}
	if err := yaml.Unmarshal(data, &qs); err != nil {
		t.Fatal(err)
	}
	for _, q := range qs {
		if strings.Contains(strings.ToLower(ClassifierInstructions), strings.ToLower(q.Question)) {
			t.Errorf("classifier prompt contains benchmark question %q", q.Question)
		}
	}
}

func TestClassifierIntegration(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("OMNI_INTEGRATION") != "1" {
		t.Skip("OMNI_INTEGRATION=1 is required for paid integration tests")
	}
	if cfg.AnthropicAPIKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	sem := loadSemantic(t)
	c := NewClassifier(llm.NewClient(cfg.AnthropicAPIKey, cfg.AnthropicWorkspaceID), sem.Render())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	d, err := c.Route(ctx, "What is the email address of the user with id 42?")
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != llm.TierCheap || d.Cost <= 0 || len(d.CallRecords) != 1 {
		t.Errorf("decision %+v", d)
	}
}
