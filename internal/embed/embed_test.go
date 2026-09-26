package embed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/config"
)

func TestKey(t *testing.T) {
	c := NewClient("key")
	if c.Key("hello") != c.Key("hello") {
		t.Fatal("same text must give the same key")
	}
	if c.Key("hello") == c.Key("world") {
		t.Fatal("different texts must give different keys")
	}
	other := NewClient("key")
	other.Model = "other-model"
	if c.Key("hello") == other.Key("hello") {
		t.Fatal("different models must give different keys")
	}
	if len(c.Key("hello")) != 64 {
		t.Fatalf("key %q is not a sha256 hex string", c.Key("hello"))
	}
}

func TestCosine(t *testing.T) {
	if got := Cosine([]float64{1, 0}, []float64{1, 0}); got != 1 {
		t.Errorf("identical = %v, want 1", got)
	}
	if got := Cosine([]float64{1, 0}, []float64{0, 1}); got != 0 {
		t.Errorf("orthogonal = %v, want 0", got)
	}
	if got := Cosine([]float64{1, 1}, []float64{-1, -1}); got < -1.000000001 || got > -0.999999999 {
		t.Errorf("opposite = %v, want -1", got)
	}
	if got := Cosine([]float64{2, 0}, []float64{1, 0}); got != 1 {
		t.Errorf("scaled = %v, want 1", got)
	}
	if got := Cosine([]float64{0, 0}, []float64{1, 0}); got != 0 {
		t.Errorf("zero vector = %v, want 0", got)
	}
	if got := Cosine([]float64{1}, []float64{1, 0}); got != 0 {
		t.Errorf("mismatched lengths = %v, want 0", got)
	}
}

func fakeEmbedServer(t *testing.T, calls *atomic.Int32, dim int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/embeddings" {
			t.Errorf("path %s, want /embeddings", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer auth header")
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != Model {
			t.Errorf("model %q, want %q", body.Model, Model)
		}
		type datum struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		data := make([]datum, len(body.Input))
		for i, text := range body.Input {
			vec := make([]float64, dim)
			for j := range vec {
				vec[j] = float64(len(text)+j+1) / 100
			}
			data[i] = datum{Index: i, Embedding: vec}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":%s,"model":%q,"usage":{"prompt_tokens":%d,"total_tokens":%d}}`,
			mustJSON(t, data), body.Model, 7*len(body.Input), 7*len(body.Input))
	}))
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func equalVecs(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEmbedCachesOnDisk(t *testing.T) {
	var calls atomic.Int32
	srv := fakeEmbedServer(t, &calls, 4)
	defer srv.Close()
	c := NewClient("test-key")
	c.BaseURL = srv.URL
	c.CacheDir = t.TempDir()
	ctx := context.Background()
	first, err := c.Embed(ctx, []string{"alpha", "beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
	if first.Tokens != 14 || first.Hits != 0 || len(first.Embeddings) != 3 {
		t.Fatalf("result %+v", first)
	}
	if !equalVecs(first.Embeddings[0], first.Embeddings[2]) {
		t.Fatal("duplicate input must return the same vector")
	}
	if len(first.Embeddings[1]) != 4 {
		t.Fatalf("vector length %d, want 4", len(first.Embeddings[1]))
	}
	entries, err := os.ReadDir(c.CacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("cache files = %d, want 2", len(entries))
	}
	second, err := c.Embed(ctx, []string{"beta", "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("cached rerun made an HTTP call, calls = %d", calls.Load())
	}
	if second.Tokens != 0 || second.Hits != 2 {
		t.Fatalf("cached result %+v", second)
	}
	if !equalVecs(second.Embeddings[0], first.Embeddings[1]) || !equalVecs(second.Embeddings[1], first.Embeddings[0]) {
		t.Fatal("cached vectors differ from the fetched ones")
	}
}

func TestEmbedBatches(t *testing.T) {
	var calls atomic.Int32
	srv := fakeEmbedServer(t, &calls, 2)
	defer srv.Close()
	c := NewClient("test-key")
	c.BaseURL = srv.URL
	c.CacheDir = t.TempDir()
	c.Batch = 2
	res, err := c.Embed(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
	if len(res.Embeddings) != 5 || res.Tokens != 35 {
		t.Fatalf("result %+v", res)
	}
}

func TestEmbedErrors(t *testing.T) {
	c := NewClient("test-key")
	c.CacheDir = t.TempDir()
	if _, err := c.Embed(context.Background(), []string{""}); err == nil {
		t.Error("empty text: want error")
	}
	if _, err := c.Embed(context.Background(), []string{"x", ""}); err == nil {
		t.Error("one empty text: want error")
	}
	res, err := c.Embed(context.Background(), nil)
	if err != nil || len(res.Embeddings) != 0 || res.Calls != 0 {
		t.Errorf("empty input = %+v %v", res, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("bad key"))
	}))
	defer srv.Close()
	c.BaseURL = srv.URL
	if _, err := c.Embed(context.Background(), []string{"x"}); err == nil {
		t.Error("401: want error")
	}
}

func TestEmbedIntegration(t *testing.T) {
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
	cache := t.TempDir()
	c := NewClient(cfg.VeniceAPIKey)
	c.CacheDir = cache
	ctx := context.Background()
	texts := []string{
		"What is the email address of the user with id 42?",
		"What share of users placed a second order within 60 days?",
	}
	first, err := c.Embed(ctx, texts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Calls != 1 || len(first.Embeddings) != 2 {
		t.Fatalf("result %+v", first)
	}
	n := len(first.Embeddings[0])
	if n == 0 || len(first.Embeddings[1]) != n {
		t.Fatalf("vector lengths %d and %d, want equal and nonzero", n, len(first.Embeddings[1]))
	}
	if first.Tokens <= 0 {
		t.Fatalf("tokens = %d, want positive", first.Tokens)
	}
	t.Logf("vector length %d, tokens %d", n, first.Tokens)
	again := NewClient(cfg.VeniceAPIKey)
	again.CacheDir = cache
	again.BaseURL = "http://127.0.0.1:9"
	second, err := again.Embed(ctx, texts)
	if err != nil {
		t.Fatalf("cached rerun must make no HTTP call: %v", err)
	}
	if second.Calls != 0 || second.Tokens != 0 || second.Hits != 2 {
		t.Fatalf("cached result %+v", second)
	}
	for i := range texts {
		if !equalVecs(first.Embeddings[i], second.Embeddings[i]) {
			t.Fatalf("cached vector %d differs", i)
		}
		if got := Cosine(first.Embeddings[i], second.Embeddings[i]); got != 1 {
			t.Fatalf("self similarity = %v, want 1", got)
		}
	}
	entries, err := os.ReadDir(filepath.Join(cache))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("cache files = %d, want 2", len(entries))
	}
}

func TestEmbedCacheHitChargesStoredTokens(t *testing.T) {
	var calls atomic.Int32
	srv := fakeEmbedServer(t, &calls, 3)
	defer srv.Close()
	c := NewClient("test-key")
	c.BaseURL = srv.URL
	c.CacheDir = t.TempDir()
	ctx := context.Background()
	cold, err := c.Embed(ctx, []string{"alpha", "a much longer question text"})
	if err != nil {
		t.Fatal(err)
	}
	warm, err := c.Embed(ctx, []string{"alpha", "a much longer question text"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || warm.Tokens != 0 || warm.Hits != 2 {
		t.Fatalf("warm result %+v calls %d", warm, calls.Load())
	}
	if warm.CachedTokens != cold.Tokens || warm.BilledTokens() != cold.BilledTokens() {
		t.Fatalf("warm billed %d, cold billed %d", warm.BilledTokens(), cold.BilledTokens())
	}
	single, err := c.Embed(ctx, []string{"alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if single.CachedTokens <= 0 || single.CachedTokens >= cold.Tokens {
		t.Fatalf("single hit tokens %d, want a share of %d", single.CachedTokens, cold.Tokens)
	}
}

func TestSplitTokensSumsToTotal(t *testing.T) {
	texts := []string{"a", "bbbb", "cc"}
	for _, total := range []int64{0, 1, 7, 100} {
		var sum int64
		for _, n := range splitTokens(total, texts) {
			sum += n
		}
		if sum != total {
			t.Errorf("splitTokens(%d) sums to %d", total, sum)
		}
	}
	if got := splitTokens(9, []string{"only"}); got[0] != 9 {
		t.Errorf("single text share %d, want 9", got[0])
	}
}

func TestEmbedRetriesTransientErrors(t *testing.T) {
	var calls atomic.Int32
	good := fakeEmbedServer(t, &calls, 2)
	defer good.Close()
	var fails atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch fails.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			good.Config.Handler.ServeHTTP(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient("test-key")
	c.BaseURL = srv.URL
	c.CacheDir = t.TempDir()
	c.Backoff = time.Millisecond
	res, err := c.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatal(err)
	}
	if fails.Load() != 3 || res.Calls != 3 || len(res.Embeddings) != 1 {
		t.Fatalf("requests %d result %+v", fails.Load(), res)
	}
}

func TestEmbedRetryLimits(t *testing.T) {
	var hits atomic.Int32
	status := http.StatusServiceUnavailable
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := NewClient("test-key")
	c.BaseURL = srv.URL
	c.CacheDir = t.TempDir()
	c.Backoff = time.Millisecond
	if _, err := c.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("persistent 503: want error")
	}
	if hits.Load() != DefaultMaxAttempts {
		t.Fatalf("requests %d, want %d", hits.Load(), DefaultMaxAttempts)
	}
	hits.Store(0)
	status = http.StatusBadRequest
	_, err := c.Embed(context.Background(), []string{"x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest || hits.Load() != 1 {
		t.Fatalf("400 must not be retried: err %v requests %d", err, hits.Load())
	}
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := closed.URL
	closed.Close()
	c.BaseURL = url
	res, err := c.Embed(context.Background(), []string{"x"})
	if err == nil || res.Calls != DefaultMaxAttempts {
		t.Fatalf("network error must be retried: err %v calls %d", err, res.Calls)
	}
}
