package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/llm"
)

const (
	ProviderName       = "jev"
	BaseURL            = "https://api.typesafe.ai/v1"
	DefaultModel       = "jev-latest"
	InputPricePerMTok  = 0.042
	OutputPricePerMTok = 0.0
	RequestTimeout     = 30 * time.Second
	DefaultMaxAttempts = 3
	DefaultBackoff     = 500 * time.Millisecond
	maxErrorBodyChars  = 500
	TypeChoice         = "choice"
	TypeScore          = "score"
	TypeNoul           = "noul"
	requestIDHeader    = "x-request-id"
	retryAfterHeader   = "retry-after"
	statusOverloaded   = 529
	maxRetryAfterWait  = 10 * time.Second
)

type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

func Choice(instructions any, options map[string]any) Question {
	return Question{Type: TypeChoice, Instructions: instructions, Criteria: options}
}

func Score(instructions any, levels []any) Question {
	return Question{Type: TypeScore, Instructions: instructions, Criteria: levels}
}

func Noul(instructions any) Question {
	return Question{Type: TypeNoul, Instructions: instructions}
}

type Request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

func (a Answer) Top() (string, float64) {
	keys := make([]string, 0, len(a.Probabilities))
	for k := range a.Probabilities {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var best string
	var bestP float64 = -1
	for _, k := range keys {
		if a.Probabilities[k] > bestP {
			best, bestP = k, a.Probabilities[k]
		}
	}
	return best, bestP
}

type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

func (r Response) Answer(id string) (Answer, error) {
	a, ok := r.Answers[id]
	if !ok {
		return Answer{}, fmt.Errorf("jev: no answer for question %q", id)
	}
	return a, nil
}

func Cost(u Usage) float64 {
	return (float64(u.InputTokens)*InputPricePerMTok + float64(u.OutputTokens)*OutputPricePerMTok) / 1e6
}

type Client struct {
	APIKey      string
	BaseURL     string
	Model       string
	HTTP        *http.Client
	MaxAttempts int
	Backoff     time.Duration
}

func NewClient(apiKey string) *Client {
	return &Client{
		APIKey:      apiKey,
		BaseURL:     BaseURL,
		Model:       DefaultModel,
		HTTP:        &http.Client{Timeout: RequestTimeout},
		MaxAttempts: DefaultMaxAttempts,
		Backoff:     DefaultBackoff,
	}
}

type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jev: status %d: %s", e.Status, e.Body)
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == statusOverloaded || status >= 500
}

func (c *Client) Ask(ctx context.Context, purpose string, state any, questions map[string]Question, trace *llm.Trace) (Response, llm.CallRecord, error) {
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	rec := llm.CallRecord{Time: time.Now().UTC(), Provider: ProviderName, Model: model, Purpose: purpose}
	start := time.Now()
	resp, err := c.do(ctx, Request{State: state, Model: model, Questions: questions}, &rec)
	rec.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		rec.Error = err.Error()
	} else {
		if resp.Model != "" {
			rec.Model = resp.Model
		}
		rec.Usage = llm.Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
		rec.CostUSD = Cost(resp.Usage)
		rec.StopReason = "answered"
	}
	if trace != nil {
		trace.Append(rec)
	}
	return resp, rec, err
}

func (c *Client) do(ctx context.Context, body Request, rec *llm.CallRecord) (Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	attempts := c.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		rec.Attempts = i + 1
		resp, wait, err := c.post(ctx, payload, rec)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		apiErr, ok := err.(*APIError)
		if ok && !retryable(apiErr.Status) {
			return Response{}, err
		}
		if ctx.Err() != nil || i == attempts-1 {
			break
		}
		if wait <= 0 {
			wait = c.Backoff << i
		}
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-time.After(wait):
		}
	}
	return Response{}, lastErr
}

func (c *Client) post(ctx context.Context, payload []byte, rec *llm.CallRecord) (Response, time.Duration, error) {
	base := c.BaseURL
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/systemone", bytes.NewReader(payload))
	if err != nil {
		return Response{}, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: RequestTimeout}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return Response{}, 0, err
	}
	defer resp.Body.Close()
	if id := resp.Header.Get(requestIDHeader); id != "" {
		rec.RequestID = id
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Response{}, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > maxErrorBodyChars {
			msg = msg[:maxErrorBodyChars]
		}
		return Response{}, retryAfter(resp.Header.Get(retryAfterHeader)), &APIError{Status: resp.StatusCode, Body: msg}
	}
	var parsed Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		return Response{}, 0, fmt.Errorf("jev: decode response: %w", err)
	}
	return parsed, 0, nil
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
