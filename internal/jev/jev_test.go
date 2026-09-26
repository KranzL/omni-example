package jev

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/llm"
)

const sampleResponse = `{"model":"jev-1.13.0","answers":{"difficulty":{"type":"choice","choice":"easy","confidence":0.99,"probabilities":{"easy":1.0,"hard":0.0,"moderate":0.0}},"depth":{"type":"score","score":0.15,"confidence":0.7,"legend":{"0":"low","1":"high"},"probabilities":{"0":0.85,"1":0.15}}},"usage":{"input_tokens":373,"output_tokens":53}}`

func testQuestions() map[string]Question {
	return map[string]Question{
		"difficulty": Choice("How hard is this?", map[string]any{"easy": "one lookup", "moderate": nil, "hard": "many steps"}),
		"depth":      Score("How deep is the analysis?", []any{"low", "high"}),
	}
}

func TestAskRequestShape(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Errorf("got %s %s, want POST /v1/systemone", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("missing bearer auth header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content type %q", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("x-request-id", "req-1")
		w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()
	c := NewClient("test-key")
	c.BaseURL = srv.URL + "/v1"
	state := map[string]any{"question": "How many users?"}
	var trace llm.Trace
	resp, rec, err := c.Ask(context.Background(), "route-jev", state, testQuestions(), &trace)
	if err != nil {
		t.Fatal(err)
	}
	if body["model"] != DefaultModel {
		t.Errorf("model %v, want %s", body["model"], DefaultModel)
	}
	if st, ok := body["state"].(map[string]any); !ok || st["question"] != "How many users?" {
		t.Errorf("state %v", body["state"])
	}
	qs := body["questions"].(map[string]any)
	d := qs["difficulty"].(map[string]any)
	if d["type"] != "choice" || d["instructions"] != "How hard is this?" {
		t.Errorf("difficulty question %v", d)
	}
	crit := d["criteria"].(map[string]any)
	if len(crit) != 3 || crit["moderate"] != nil || crit["easy"] != "one lookup" {
		t.Errorf("choice criteria %v", crit)
	}
	s := qs["depth"].(map[string]any)
	if s["type"] != "score" || len(s["criteria"].([]any)) != 2 {
		t.Errorf("score question %v", s)
	}
	a, err := resp.Answer("difficulty")
	if err != nil {
		t.Fatal(err)
	}
	if a.Choice != "easy" || a.Confidence != 0.99 || a.Probabilities["easy"] != 1.0 {
		t.Errorf("choice answer %+v", a)
	}
	sc, _ := resp.Answer("depth")
	if sc.Score != 0.15 || sc.Legend["1"] != "high" || sc.Confidence != 0.7 {
		t.Errorf("score answer %+v", sc)
	}
	if _, err := resp.Answer("missing"); err == nil {
		t.Error("missing answer id must error")
	}
	if rec.Provider != ProviderName || rec.Model != "jev-1.13.0" || rec.Purpose != "route-jev" || rec.RequestID != "req-1" || rec.Attempts != 1 {
		t.Errorf("record %+v", rec)
	}
	if rec.Usage.InputTokens != 373 || rec.Usage.OutputTokens != 53 {
		t.Errorf("usage %+v", rec.Usage)
	}
	want := 373 * 0.042 / 1e6
	if math.Abs(rec.CostUSD-want) > 1e-15 {
		t.Errorf("cost %v, want %v", rec.CostUSD, want)
	}
	if len(trace.Records()) != 1 || trace.TotalCost() != rec.CostUSD {
		t.Errorf("trace not updated: %+v", trace.Records())
	}
}

func TestCostChargesInputOnly(t *testing.T) {
	if got := Cost(Usage{InputTokens: 1_000_000, OutputTokens: 5_000_000}); math.Abs(got-0.042) > 1e-12 {
		t.Errorf("cost %v, want 0.042", got)
	}
	if got := Cost(Usage{OutputTokens: 1000}); got != 0 {
		t.Errorf("output-only cost %v, want 0", got)
	}
}

func TestAskRetriesOverloaded(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(529)
			w.Write([]byte(`{"detail":"overloaded"}`))
			return
		}
		w.Write([]byte(sampleResponse))
	}))
	defer srv.Close()
	c := NewClient("k")
	c.BaseURL = srv.URL
	c.Backoff = time.Millisecond
	_, rec, err := c.Ask(context.Background(), "p", "s", testQuestions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 || rec.Attempts != 3 {
		t.Errorf("calls %d attempts %d, want 3", calls.Load(), rec.Attempts)
	}
}

func TestAskDoesNotRetryValidationError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"detail":"criteria required"}`))
	}))
	defer srv.Close()
	c := NewClient("k")
	c.BaseURL = srv.URL
	c.Backoff = time.Millisecond
	var trace llm.Trace
	_, rec, err := c.Ask(context.Background(), "p", "s", testQuestions(), &trace)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("err %v, want 422 APIError", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls %d, want 1", calls.Load())
	}
	if rec.Error == "" || rec.CostUSD != 0 || len(trace.Records()) != 1 {
		t.Errorf("failed call must be traced with an error and no cost: %+v", rec)
	}
}

func TestRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{"": 0, "2": 2 * time.Second, "0.5": 500 * time.Millisecond, "x": 0, "600": maxRetryAfterWait}
	for in, want := range cases {
		if got := retryAfter(in); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestAnswerTop(t *testing.T) {
	a := Answer{Probabilities: map[string]float64{"accept": 0.3, "reject": 0.7}}
	if k, p := a.Top(); k != "reject" || p != 0.7 {
		t.Errorf("top %s %v", k, p)
	}
}

func TestIntegrationSystemOne(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.JevAPIKey == "" || os.Getenv("JEV_INTEGRATION") == "" {
		t.Skip("set JEV_API_KEY and JEV_INTEGRATION=1 to call the live endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	resp, rec, err := NewClient(cfg.JevAPIKey).Ask(ctx, "integration", "How many users have country Brasil?", testQuestions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := resp.Answer("difficulty")
	if err != nil {
		t.Fatal(err)
	}
	if a.Choice == "" || a.Confidence <= 0 || rec.Usage.InputTokens == 0 {
		t.Errorf("answer %+v record %+v", a, rec)
	}
}
