package router

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/embed"
	"github.com/KranzL/omni-example/internal/llm"
)

func seedFilePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "bench", "router_seed.yaml")
}

func TestLoadSeeds(t *testing.T) {
	seeds, err := LoadSeeds(seedFilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, s := range seeds {
		counts[s.Label]++
	}
	if len(seeds) < 36 {
		t.Errorf("seeds = %d, want at least 36", len(seeds))
	}
	for _, label := range []string{LabelEasy, LabelModerate, LabelHard} {
		if counts[label] < MinSeedsPerLabel {
			t.Errorf("%s seeds = %d, want at least %d", label, counts[label], MinSeedsPerLabel)
		}
	}
}

func TestLoadSeedsRejects(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "seeds.yaml")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := func(id, label string) string {
		return fmt.Sprintf("- id: %s\n  difficulty: %s\n  question: Question %s?\n", id, label, id)
	}
	var full string
	for i := 0; i < MinSeedsPerLabel; i++ {
		full += good(fmt.Sprintf("e%02d", i), LabelEasy)
		full += good(fmt.Sprintf("m%02d", i), LabelModerate)
		full += good(fmt.Sprintf("h%02d", i), LabelHard)
	}
	if _, err := LoadSeeds(write(t, full)); err != nil {
		t.Fatalf("full set: %v", err)
	}
	tooFew := strings.Replace(full, good("e00", LabelEasy), "", 1)
	if _, err := LoadSeeds(write(t, tooFew)); err == nil {
		t.Error("11 easy seeds: want error")
	}
	dup := full + good("e01", LabelEasy)
	if _, err := LoadSeeds(write(t, dup)); err == nil {
		t.Error("duplicate id: want error")
	}
	badLabel := full + good("x99", "expert")
	if _, err := LoadSeeds(write(t, badLabel)); err == nil {
		t.Error("expert label: want error")
	}
	if _, err := LoadSeeds(write(t, full+"- id: z99\n  difficulty: easy\n  question: \"\"\n")); err == nil {
		t.Error("empty question: want error")
	}
	if _, err := LoadSeeds(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("missing file: want error")
	}
}

func TestSeedsIndependentOfBenchmark(t *testing.T) {
	seeds, err := LoadSeeds(seedFilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "bench", "questions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var qs []struct {
		ID       string `yaml:"id"`
		Question string `yaml:"question"`
	}
	if err := yaml.Unmarshal(data, &qs); err != nil {
		t.Fatal(err)
	}
	for _, s := range seeds {
		for _, q := range qs {
			if strings.EqualFold(strings.TrimSpace(s.Question), strings.TrimSpace(q.Question)) {
				t.Errorf("seed %s duplicates benchmark question %s", s.ID, q.ID)
			}
		}
	}
}

type stubEmbedder struct {
	vecs  map[string][]float64
	calls int
}

func (s *stubEmbedder) Embed(ctx context.Context, inputs []string) (embed.Result, error) {
	s.calls++
	vecs := make([][]float64, len(inputs))
	for i, text := range inputs {
		v, ok := s.vecs[text]
		if !ok {
			return embed.Result{}, fmt.Errorf("stub: no vector for %q", text)
		}
		vecs[i] = v
	}
	return embed.Result{Embeddings: vecs, Tokens: int64(5 * len(inputs)), Calls: 1}, nil
}

func stubSeeds() []Seed {
	return []Seed{
		{ID: "e1", Label: LabelEasy, Question: "easy one"},
		{ID: "e2", Label: LabelEasy, Question: "easy two"},
		{ID: "m1", Label: LabelModerate, Question: "moderate one"},
		{ID: "m2", Label: LabelModerate, Question: "moderate two"},
		{ID: "h1", Label: LabelHard, Question: "hard one"},
		{ID: "h2", Label: LabelHard, Question: "hard two"},
	}
}

func TestVote(t *testing.T) {
	nb := []Neighbor{
		{ID: "h1", Label: LabelHard, Similarity: 0.9},
		{ID: "m1", Label: LabelModerate, Similarity: 0.8},
		{ID: "m2", Label: LabelModerate, Similarity: 0.7},
		{ID: "e1", Label: LabelEasy, Similarity: 0.1},
	}
	if got := Vote(nb); got != LabelModerate {
		t.Errorf("vote = %s, want moderate (0.8+0.7 beats 0.9)", got)
	}
	tied := []Neighbor{
		{ID: "h1", Label: LabelHard, Similarity: 0.8},
		{ID: "e1", Label: LabelEasy, Similarity: 0.8},
	}
	if got := Vote(tied); got != LabelHard {
		t.Errorf("tied vote = %s, want nearest label hard", got)
	}
	neg := []Neighbor{
		{ID: "h1", Label: LabelHard, Similarity: -0.5},
		{ID: "e1", Label: LabelEasy, Similarity: 0.2},
	}
	if got := Vote(neg); got != LabelEasy {
		t.Errorf("negative vote = %s, want easy", got)
	}
}

func TestEmbeddingRoutes(t *testing.T) {
	stub := &stubEmbedder{vecs: map[string][]float64{
		"easy one":     {1, 0},
		"easy two":     {0.9, 0.1},
		"moderate one": {0, 1},
		"moderate two": {0.1, 0.9},
		"hard one":     {-1, 0},
		"hard two":     {-0.9, -0.1},
		"which cohort had the highest repeat rate": {-1, 0.05},
	}}
	r := NewEmbedding(stub, llm.VeniceEmbedBgeM3, stubSeeds(), EmbeddingParams{K: 3, Threshold: 0.6, FallbackTier: llm.TierMid})
	d, err := r.Route(context.Background(), "which cohort had the highest repeat rate")
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != llm.TierTop || d.Label != LabelHard {
		t.Errorf("decision %+v, want top/hard", d)
	}
	wantCost, err := llm.Cost(llm.VeniceEmbedBgeM3, llm.Usage{InputTokens: 5})
	if err != nil {
		t.Fatal(err)
	}
	if d.Cost != wantCost {
		t.Errorf("cost %v, want %v", d.Cost, wantCost)
	}
	if len(d.CallRecords) != 1 {
		t.Fatalf("records %+v", d.CallRecords)
	}
	rec := d.CallRecords[0]
	if rec.Purpose != PurposeEmbedding || rec.Model != llm.VeniceEmbedBgeM3 || rec.Provider != llm.ProviderVenice {
		t.Errorf("record %+v", rec)
	}
	if rec.Usage.InputTokens != 5 || rec.CostUSD != wantCost {
		t.Errorf("record usage %+v cost %v", rec.Usage, rec.CostUSD)
	}
	if d.Reason == "" || strings.Contains(d.Reason, "fallback") {
		t.Errorf("reason %q", d.Reason)
	}
	if stub.calls != 2 {
		t.Errorf("embed calls = %d, want 2 (seeds plus question)", stub.calls)
	}
}

func TestEmbeddingFallback(t *testing.T) {
	stub := &stubEmbedder{vecs: map[string][]float64{
		"easy one":     {1, 0},
		"easy two":     {0.9, 0.1},
		"moderate one": {0, 1},
		"moderate two": {0.1, 0.9},
		"hard one":     {-1, 0},
		"hard two":     {-0.9, -0.1},
		"unrelated":    {0, -1},
	}}
	r := NewEmbedding(stub, llm.VeniceEmbedBgeM3, stubSeeds(), DefaultEmbeddingParams())
	d, err := r.Route(context.Background(), "unrelated")
	if err != nil {
		t.Fatal(err)
	}
	if d.Tier != llm.TierMid || d.Label != LabelModerate {
		t.Errorf("decision %+v, want mid/moderate fallback", d)
	}
	if !strings.Contains(d.Reason, "fallback") {
		t.Errorf("reason %q, want fallback marker", d.Reason)
	}
	if d.Cost <= 0 || len(d.CallRecords) != 1 {
		t.Errorf("fallback must still record overhead %+v", d)
	}
}

func TestEmbeddingWarmErrors(t *testing.T) {
	stub := &stubEmbedder{vecs: map[string][]float64{}}
	if err := NewEmbedding(stub, llm.VeniceEmbedBgeM3, nil, DefaultEmbeddingParams()).Warm(context.Background()); err == nil {
		t.Error("no seeds: want error")
	}
	if err := NewEmbedding(stub, llm.VeniceEmbedBgeM3, stubSeeds(), EmbeddingParams{K: 0, Threshold: 0.6, FallbackTier: llm.TierMid}).Warm(context.Background()); err == nil {
		t.Error("k=0: want error")
	}
	if err := NewEmbedding(stub, llm.VeniceEmbedBgeM3, stubSeeds(), EmbeddingParams{K: 5, Threshold: 0.6, FallbackTier: "nano"}).Warm(context.Background()); err == nil {
		t.Error("bad fallback tier: want error")
	}
	bad := &stubEmbedder{vecs: map[string][]float64{
		"easy one": {1, 0}, "easy two": {1}, "moderate one": {0, 1},
		"moderate two": {0, 1}, "hard one": {0, 1}, "hard two": {0, 1},
	}}
	if err := NewEmbedding(bad, llm.VeniceEmbedBgeM3, stubSeeds(), DefaultEmbeddingParams()).Warm(context.Background()); err == nil {
		t.Error("ragged vectors: want error")
	}
	if _, err := NewEmbedding(stub, llm.VeniceEmbedBgeM3, stubSeeds(), DefaultEmbeddingParams()).Route(context.Background(), "easy one"); err == nil {
		t.Error("warm failure must fail the route")
	}
}

type scriptedEmbedder struct {
	base  *stubEmbedder
	errs  []error
	edit  func(inputs []string, res embed.Result) embed.Result
	calls int
}

func (s *scriptedEmbedder) Embed(ctx context.Context, inputs []string) (embed.Result, error) {
	i := s.calls
	s.calls++
	if i < len(s.errs) && s.errs[i] != nil {
		return embed.Result{}, s.errs[i]
	}
	res, err := s.base.Embed(ctx, inputs)
	if err == nil && s.edit != nil {
		res = s.edit(inputs, res)
	}
	return res, err
}

func stubSeedVectors() map[string][]float64 {
	return map[string][]float64{
		"easy one":     {1, 0},
		"easy two":     {0.9, 0.1},
		"moderate one": {0, 1},
		"moderate two": {0.1, 0.9},
		"hard one":     {-1, 0},
		"hard two":     {-0.9, -0.1},
		"question":     {1, 0.05},
	}
}

func TestEmbeddingWarmFailureIsRetried(t *testing.T) {
	s := &scriptedEmbedder{base: &stubEmbedder{vecs: stubSeedVectors()}, errs: []error{fmt.Errorf("venice 503")}}
	r := NewEmbedding(s, llm.VeniceEmbedBgeM3, stubSeeds(), DefaultEmbeddingParams())
	if _, err := r.Route(context.Background(), "question"); err == nil {
		t.Fatal("first route: want the warm-up error")
	}
	d, err := r.Route(context.Background(), "question")
	if err != nil {
		t.Fatalf("second route must retry the warm-up: %v", err)
	}
	if d.Tier != llm.TierCheap || s.calls != 3 {
		t.Errorf("decision %+v calls %d", d, s.calls)
	}
}

func TestEmbeddingRejectsBadQuestionVectors(t *testing.T) {
	cases := map[string]func(inputs []string, res embed.Result) embed.Result{
		"none": func(inputs []string, res embed.Result) embed.Result {
			if len(inputs) == 1 {
				res.Embeddings = nil
			}
			return res
		},
		"two": func(inputs []string, res embed.Result) embed.Result {
			if len(inputs) == 1 {
				res.Embeddings = append(res.Embeddings, res.Embeddings[0])
			}
			return res
		},
		"dimension": func(inputs []string, res embed.Result) embed.Result {
			if len(inputs) == 1 {
				res.Embeddings = [][]float64{{1, 0, 0}}
			}
			return res
		},
	}
	for name, edit := range cases {
		s := &scriptedEmbedder{base: &stubEmbedder{vecs: stubSeedVectors()}, edit: edit}
		r := NewEmbedding(s, llm.VeniceEmbedBgeM3, stubSeeds(), DefaultEmbeddingParams())
		if _, err := r.Route(context.Background(), "question"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestEmbeddingChargesCachedTokens(t *testing.T) {
	edit := func(inputs []string, res embed.Result) embed.Result {
		res.CachedTokens = res.Tokens
		res.Tokens = 0
		res.Calls = 0
		res.Hits = len(inputs)
		return res
	}
	s := &scriptedEmbedder{base: &stubEmbedder{vecs: stubSeedVectors()}, edit: edit}
	r := NewEmbedding(s, llm.VeniceEmbedBgeM3, stubSeeds(), DefaultEmbeddingParams())
	d, err := r.Route(context.Background(), "question")
	if err != nil {
		t.Fatal(err)
	}
	want, err := llm.Cost(llm.VeniceEmbedBgeM3, llm.Usage{InputTokens: 5})
	if err != nil {
		t.Fatal(err)
	}
	if d.Cost != want || d.CallRecords[0].Usage.InputTokens != 5 || d.CallRecords[0].CostUSD != want {
		t.Errorf("cached route cost %v record %+v, want %v", d.Cost, d.CallRecords[0], want)
	}
	seedWant, err := llm.Cost(llm.VeniceEmbedBgeM3, llm.Usage{InputTokens: 30})
	if err != nil {
		t.Fatal(err)
	}
	if r.SeedCost != seedWant {
		t.Errorf("cached seed cost %v, want %v", r.SeedCost, seedWant)
	}
}

func TestEmbeddingSeedsDiverseIntegration(t *testing.T) {
	if os.Getenv("OMNI_INTEGRATION") != "1" {
		t.Skip("OMNI_INTEGRATION=1 is required for paid integration tests")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VeniceAPIKey == "" {
		t.Skip("VENICE_API_KEY not set")
	}
	seeds, err := LoadSeeds(seedFilePath(t))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "bench", "questions.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var qs []struct {
		ID       string `yaml:"id"`
		Question string `yaml:"question"`
	}
	if err := yaml.Unmarshal(data, &qs); err != nil {
		t.Fatal(err)
	}
	texts := make([]string, 0, len(seeds)+len(qs))
	for _, s := range seeds {
		texts = append(texts, s.Question)
	}
	for _, q := range qs {
		texts = append(texts, q.Question)
	}
	c := embed.NewClient(cfg.VeniceAPIKey)
	c.CacheDir = t.TempDir()
	res, err := c.Embed(context.Background(), texts)
	if err != nil {
		t.Fatal(err)
	}
	seedVecs := res.Embeddings[:len(seeds)]
	benchVecs := res.Embeddings[len(seeds):]
	worst := 0.0
	for i, s := range seeds {
		best := -1.0
		bestID := ""
		for j, q := range qs {
			sim := embed.Cosine(seedVecs[i], benchVecs[j])
			if sim > best {
				best = sim
				bestID = q.ID
			}
		}
		if best > worst {
			worst = best
		}
		t.Logf("seed %s max similarity %.4f to %s", s.ID, best, bestID)
		if best > 0.95 {
			t.Errorf("seed %s too close to benchmark question %s: %.4f", s.ID, bestID, best)
		}
	}
	t.Logf("worst seed similarity %.4f over %d seeds and %d benchmark questions", worst, len(seeds), len(qs))
}

func TestLoadSeedsNormalizesBirdLabels(t *testing.T) {
	var body string
	for i := 0; i < MinSeedsPerLabel; i++ {
		body += fmt.Sprintf("- id: s%02d\n  difficulty: simple\n  question: Simple %d?\n", i, i)
		body += fmt.Sprintf("- id: m%02d\n  difficulty: moderate\n  question: Moderate %d?\n", i, i)
		body += fmt.Sprintf("- id: c%02d\n  difficulty: challenging\n  question: Challenging %d?\n", i, i)
	}
	path := filepath.Join(t.TempDir(), "seeds.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	seeds, err := LoadSeeds(path)
	if err != nil {
		t.Fatalf("bird labels: %v", err)
	}
	counts := map[string]int{}
	for _, s := range seeds {
		counts[s.Label]++
	}
	for label, want := range map[string]int{LabelEasy: MinSeedsPerLabel, LabelModerate: MinSeedsPerLabel, LabelHard: MinSeedsPerLabel} {
		if counts[label] != want {
			t.Fatalf("%s = %d, want %d", label, counts[label], want)
		}
	}
}

func TestLoadBirdSeedsFile(t *testing.T) {
	path := filepath.Join("..", "..", "bench", "bird", "router_seed.yaml")
	if _, err := os.Stat(path); err != nil {
		t.Skip("bird seeds not present")
	}
	seeds, err := LoadSeeds(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 36 {
		t.Fatalf("seeds = %d, want 36", len(seeds))
	}
}
