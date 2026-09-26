package router

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/KranzL/omni-example/internal/embed"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	NameEmbedding    = "embedding"
	PurposeEmbedding = "route-embedding"
	MinSeedsPerLabel = 12
)

type EmbeddingParams struct {
	K            int      `json:"k"`
	Threshold    float64  `json:"threshold"`
	FallbackTier llm.Tier `json:"fallback_tier"`
}

func DefaultEmbeddingParams() EmbeddingParams {
	return EmbeddingParams{K: 5, Threshold: 0.6, FallbackTier: llm.TierMid}
}

type Seed struct {
	ID       string    `yaml:"id"`
	Label    string    `yaml:"difficulty"`
	Question string    `yaml:"question"`
	Vector   []float64 `yaml:"-"`
}

func LoadSeeds(path string) ([]Seed, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var seeds []Seed
	if err := yaml.Unmarshal(data, &seeds); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	counts := map[string]int{}
	for i := range seeds {
		s := &seeds[i]
		if s.ID == "" {
			return nil, fmt.Errorf("seed %d: empty id", i)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("seed %s: duplicate id", s.ID)
		}
		seen[s.ID] = true
		canonical, err := CanonicalLabel(s.Label)
		if err != nil {
			return nil, fmt.Errorf("seed %s: %w", s.ID, err)
		}
		s.Label = canonical
		if s.Question == "" {
			return nil, fmt.Errorf("seed %s: empty question", s.ID)
		}
		counts[s.Label]++
	}
	for _, label := range []string{LabelEasy, LabelModerate, LabelHard} {
		if counts[label] < MinSeedsPerLabel {
			return nil, fmt.Errorf("seed set has %d %s seeds, want at least %d", counts[label], label, MinSeedsPerLabel)
		}
	}
	return seeds, nil
}

type Embedder interface {
	Embed(ctx context.Context, inputs []string) (embed.Result, error)
}

type Neighbor struct {
	ID         string
	Label      string
	Similarity float64
}

type Embedding struct {
	Embedder  Embedder
	Model     string
	Seeds     []Seed
	Params    EmbeddingParams
	SeedCost  float64
	SeedCalls int

	mu     sync.Mutex
	warmed bool
}

func NewEmbedding(e Embedder, model string, seeds []Seed, params EmbeddingParams) *Embedding {
	return &Embedding{Embedder: e, Model: model, Seeds: seeds, Params: params}
}

func (r *Embedding) Name() string {
	return NameEmbedding
}

func (r *Embedding) Warm(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.warmLocked(ctx)
}

func (r *Embedding) warmLocked(ctx context.Context) error {
	if r.warmed {
		return nil
	}
	if err := r.doWarm(ctx); err != nil {
		return err
	}
	r.warmed = true
	return nil
}

func (r *Embedding) doWarm(ctx context.Context) error {
	if len(r.Seeds) == 0 {
		return fmt.Errorf("embedding: no seeds")
	}
	if r.Params.K < 1 {
		return fmt.Errorf("embedding: k=%d, want at least 1", r.Params.K)
	}
	if _, err := labelForTier(r.Params.FallbackTier); err != nil {
		return err
	}
	texts := make([]string, len(r.Seeds))
	for i, s := range r.Seeds {
		texts[i] = s.Question
	}
	res, err := r.Embedder.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if len(res.Embeddings) != len(r.Seeds) {
		return fmt.Errorf("embedding: got %d seed vectors for %d seeds", len(res.Embeddings), len(r.Seeds))
	}
	n := len(res.Embeddings[0])
	if n == 0 {
		return fmt.Errorf("embedding: empty seed vector")
	}
	for i, vec := range res.Embeddings {
		if len(vec) != n {
			return fmt.Errorf("embedding: seed %s has vector length %d, want %d", r.Seeds[i].ID, len(vec), n)
		}
		r.Seeds[i].Vector = vec
	}
	cost, err := llm.Cost(r.Model, llm.Usage{InputTokens: res.BilledTokens()})
	if err != nil {
		return err
	}
	r.SeedCost = cost
	r.SeedCalls = res.Calls
	return nil
}

func (r *Embedding) Route(ctx context.Context, question string) (Decision, error) {
	start := time.Now()
	r.mu.Lock()
	err := r.warmLocked(ctx)
	r.mu.Unlock()
	d := Decision{Latency: time.Since(start)}
	if err != nil {
		return d, err
	}
	res, err := r.Embedder.Embed(ctx, []string{question})
	if err != nil {
		return d, err
	}
	if len(res.Embeddings) != 1 {
		return d, fmt.Errorf("embedding: got %d question vectors, want 1", len(res.Embeddings))
	}
	vec := res.Embeddings[0]
	if want := len(r.Seeds[0].Vector); len(vec) != want {
		return d, fmt.Errorf("embedding: question vector length %d, seed vector length %d", len(vec), want)
	}
	tokens := res.BilledTokens()
	cost, costErr := llm.Cost(r.Model, llm.Usage{InputTokens: tokens})
	if costErr != nil {
		return d, costErr
	}
	neighbors := make([]Neighbor, len(r.Seeds))
	for i, s := range r.Seeds {
		neighbors[i] = Neighbor{ID: s.ID, Label: s.Label, Similarity: embed.Cosine(vec, s.Vector)}
	}
	sort.SliceStable(neighbors, func(i, j int) bool { return neighbors[i].Similarity > neighbors[j].Similarity })
	k := r.Params.K
	if k > len(neighbors) {
		k = len(neighbors)
	}
	best := neighbors[0].Similarity
	label := Vote(neighbors[:k])
	fallback := false
	if best < r.Params.Threshold {
		fallback = true
		label, _ = labelForTier(r.Params.FallbackTier)
	}
	tier, err := TierForLabel(label)
	if err != nil {
		return d, err
	}
	weights := weightsOf(neighbors[:k])
	d.Tier = tier
	d.Label = label
	d.Reason = describeVote(r.Params.K, neighbors[0], weights, label, fallback, r.Params.Threshold)
	d.Cost = cost
	d.Latency = time.Since(start)
	d.CallRecords = []llm.CallRecord{{
		Time:      start.UTC(),
		Provider:  llm.ProviderVenice,
		Model:     r.Model,
		Tier:      tier,
		Purpose:   PurposeEmbedding,
		Usage:     llm.Usage{InputTokens: tokens},
		CostUSD:   cost,
		LatencyMS: d.Latency.Milliseconds(),
		Attempts:  res.Calls,
	}}
	return d, nil
}

func Vote(neighbors []Neighbor) string {
	weights := weightsOf(neighbors)
	best := ""
	top := -1.0
	for _, label := range []string{LabelEasy, LabelModerate, LabelHard} {
		if weights[label] > top {
			top = weights[label]
			best = label
		}
	}
	tied := 0
	for _, label := range []string{LabelEasy, LabelModerate, LabelHard} {
		if weights[label] == top {
			tied++
		}
	}
	if tied > 1 && len(neighbors) > 0 {
		return neighbors[0].Label
	}
	return best
}

func weightsOf(neighbors []Neighbor) map[string]float64 {
	weights := map[string]float64{LabelEasy: 0, LabelModerate: 0, LabelHard: 0}
	for _, nb := range neighbors {
		w := nb.Similarity
		if w < 0 {
			w = 0
		}
		weights[nb.Label] += w
	}
	return weights
}

func describeVote(k int, top Neighbor, weights map[string]float64, label string, fallback bool, threshold float64) string {
	s := fmt.Sprintf("k=%d top=%s:%.3f easy=%.2f moderate=%.2f hard=%.2f -> %s",
		k, top.ID, top.Similarity, weights[LabelEasy], weights[LabelModerate], weights[LabelHard], label)
	if fallback {
		s += fmt.Sprintf(" fallback best %.3f < %.2f", top.Similarity, threshold)
	}
	return s
}

func labelForTier(tier llm.Tier) (string, error) {
	switch tier {
	case llm.TierCheap:
		return LabelEasy, nil
	case llm.TierMid:
		return LabelModerate, nil
	case llm.TierTop:
		return LabelHard, nil
	default:
		return "", fmt.Errorf("embedding: unknown fallback tier %q", string(tier))
	}
}
