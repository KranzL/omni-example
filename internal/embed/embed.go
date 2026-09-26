package embed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	Model              = "text-embedding-bge-m3"
	BaseURL            = "https://api.venice.ai/api/v1"
	DefaultCacheDir    = "results/embeddings"
	MaxBatch           = 2048
	RequestTimeout     = 2 * time.Minute
	DefaultMaxAttempts = 3
	DefaultBackoff     = 500 * time.Millisecond
	maxErrorBodyChars  = 500
	maxRetryAfterWait  = 30 * time.Second
	statusOverloaded   = 529
)

type Client struct {
	APIKey      string
	BaseURL     string
	Model       string
	CacheDir    string
	Batch       int
	HTTP        *http.Client
	MaxAttempts int
	Backoff     time.Duration
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:      apiKey,
		BaseURL:     BaseURL,
		Model:       Model,
		CacheDir:    DefaultCacheDir,
		Batch:       MaxBatch,
		HTTP:        &http.Client{Timeout: RequestTimeout},
		MaxAttempts: DefaultMaxAttempts,
		Backoff:     DefaultBackoff,
	}
}

type Result struct {
	Embeddings   [][]float64
	Tokens       int64
	CachedTokens int64
	Calls        int
	Hits         int
}

func (r Result) BilledTokens() int64 {
	return r.Tokens + r.CachedTokens
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("embed: status %d: %s", e.Status, e.Body)
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == statusOverloaded || status >= 500
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedData struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

type embedResponse struct {
	Data  []embedData `json:"data"`
	Usage struct {
		PromptTokens int64 `json:"prompt_tokens"`
		TotalTokens  int64 `json:"total_tokens"`
	} `json:"usage"`
}

type cachedVector struct {
	Model     string    `json:"model"`
	Text      string    `json:"text"`
	Tokens    int64     `json:"tokens,omitempty"`
	Embedding []float64 `json:"embedding"`
}

func (c *Client) Key(text string) string {
	sum := sha256.Sum256([]byte(c.Model + "\x00" + text))
	return hex.EncodeToString(sum[:])
}

func (c *Client) path(text string) string {
	return filepath.Join(c.CacheDir, c.Key(text)+".json")
}

func (c *Client) batchSize() int {
	if c.Batch <= 0 {
		return MaxBatch
	}
	return c.Batch
}

func (c *Client) Embed(ctx context.Context, inputs []string) (Result, error) {
	var out Result
	if len(inputs) == 0 {
		return out, nil
	}
	for _, text := range inputs {
		if text == "" {
			return out, fmt.Errorf("embed: empty input text")
		}
	}
	out.Embeddings = make([][]float64, len(inputs))
	var missing []string
	positions := map[string][]int{}
	for i, text := range inputs {
		cached, ok, err := c.load(text)
		if err != nil {
			return out, err
		}
		if ok {
			out.Embeddings[i] = cached.Embedding
			out.CachedTokens += cached.Tokens
			out.Hits++
			continue
		}
		if _, dup := positions[text]; !dup {
			missing = append(missing, text)
		}
		positions[text] = append(positions[text], i)
	}
	for start := 0; start < len(missing); start += c.batchSize() {
		end := start + c.batchSize()
		if end > len(missing) {
			end = len(missing)
		}
		batch := missing[start:end]
		vecs, tokens, calls, err := c.do(ctx, batch)
		out.Calls += calls
		if err != nil {
			return out, err
		}
		out.Tokens += tokens
		shares := splitTokens(tokens, batch)
		for i, vec := range vecs {
			text := batch[i]
			if err := c.store(text, vec, shares[i]); err != nil {
				return out, err
			}
			for _, pos := range positions[text] {
				out.Embeddings[pos] = vec
			}
		}
	}
	return out, nil
}

func splitTokens(total int64, texts []string) []int64 {
	out := make([]int64, len(texts))
	if len(texts) == 0 || total <= 0 {
		return out
	}
	var chars int64
	for _, t := range texts {
		chars += int64(len(t))
	}
	var assigned int64
	for i, t := range texts {
		out[i] = total * int64(len(t)) / chars
		assigned += out[i]
	}
	for i := 0; assigned < total; i = (i + 1) % len(out) {
		out[i]++
		assigned++
	}
	return out
}

func (c *Client) load(text string) (cachedVector, bool, error) {
	data, err := os.ReadFile(c.path(text))
	if err != nil {
		if os.IsNotExist(err) {
			return cachedVector{}, false, nil
		}
		return cachedVector{}, false, err
	}
	var cached cachedVector
	if err := json.Unmarshal(data, &cached); err != nil {
		return cachedVector{}, false, err
	}
	if cached.Model != c.Model || cached.Text != text || len(cached.Embedding) == 0 {
		return cachedVector{}, false, nil
	}
	return cached, true, nil
}

func (c *Client) store(text string, vec []float64, tokens int64) error {
	if err := os.MkdirAll(c.CacheDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cachedVector{Model: c.Model, Text: text, Tokens: tokens, Embedding: vec})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.CacheDir, "vec-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, c.path(text))
}

func (c *Client) do(ctx context.Context, inputs []string) ([][]float64, int64, int, error) {
	payload, err := json.Marshal(embedRequest{Model: c.Model, Input: inputs})
	if err != nil {
		return nil, 0, 0, err
	}
	attempts := c.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	calls := 0
	for i := 0; i < attempts; i++ {
		calls++
		vecs, tokens, wait, err := c.post(ctx, payload, len(inputs))
		if err == nil {
			return vecs, tokens, calls, nil
		}
		lastErr = err
		apiErr, ok := err.(*APIError)
		if ok && !retryable(apiErr.Status) {
			return nil, 0, calls, err
		}
		if ctx.Err() != nil || i == attempts-1 {
			break
		}
		if wait <= 0 {
			wait = c.Backoff << i
		}
		select {
		case <-ctx.Done():
			return nil, 0, calls, ctx.Err()
		case <-time.After(wait):
		}
	}
	return nil, 0, calls, lastErr
}

func (c *Client) post(ctx context.Context, payload []byte, n int) ([][]float64, int64, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: RequestTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > maxErrorBodyChars {
			msg = msg[:maxErrorBodyChars]
		}
		return nil, 0, retryAfter(resp.Header.Get("Retry-After")), &APIError{Status: resp.StatusCode, Body: msg}
	}
	var parsed embedResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, 0, 0, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(parsed.Data) != n {
		return nil, 0, 0, fmt.Errorf("embed: got %d vectors for %d inputs", len(parsed.Data), n)
	}
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })
	vecs := make([][]float64, n)
	for i, d := range parsed.Data {
		if d.Index != i || len(d.Embedding) == 0 {
			return nil, 0, 0, fmt.Errorf("embed: bad vector at position %d", i)
		}
		vecs[i] = d.Embedding
	}
	tokens := parsed.Usage.PromptTokens
	if tokens == 0 {
		tokens = parsed.Usage.TotalTokens
	}
	return vecs, tokens, 0, nil
}

func retryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	var secs float64
	if _, err := fmt.Sscanf(v, "%g", &secs); err != nil || secs <= 0 {
		return 0
	}
	d := time.Duration(secs * float64(time.Second))
	if d > maxRetryAfterWait {
		d = maxRetryAfterWait
	}
	return d
}

func Cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
