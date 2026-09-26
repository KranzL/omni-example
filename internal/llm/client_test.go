package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const fakeMessage = `{
  "id": "msg_test",
  "type": "message",
  "role": "assistant",
  "model": "claude-haiku-4-5-20251001",
  "content": [{"type": "text", "text": "4"}],
  "stop_reason": "end_turn",
  "usage": {
    "input_tokens": 20,
    "output_tokens": 5,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0
  }
}`

func fakeServer(t *testing.T, failures int32, failStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		n := calls.Add(1)
		w.Header().Set("request-id", "req_"+string(rune('0'+n)))
		w.Header().Set("retry-after-ms", "1")
		w.Header().Set("content-type", "application/json")
		if n <= failures {
			w.WriteHeader(failStatus)
			io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`)
			return
		}
		io.WriteString(w, fakeMessage)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func testRequest() Request {
	return Request{
		Tier:     TierCheap,
		Purpose:  "test",
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("2+2?"))},
	}
}

func TestCallRetriesAndRecords(t *testing.T) {
	srv, calls := fakeServer(t, 2, 529)
	c := NewClient("test-key", "", option.WithBaseURL(srv.URL))
	var trace Trace
	msg, rec, err := c.Call(context.Background(), testRequest(), &trace)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != "msg_test" || calls.Load() != 3 {
		t.Fatalf("id %s calls %d", msg.ID, calls.Load())
	}
	if rec.Attempts != 3 || rec.RequestID != "req_3" || rec.StopReason != "end_turn" {
		t.Fatalf("record %+v", rec)
	}
	if rec.Tier != TierCheap || rec.Purpose != "test" || rec.Model != "claude-haiku-4-5-20251001" {
		t.Fatalf("record %+v", rec)
	}
	approx(t, rec.CostUSD, (20*1.0+5*5.0)/1e6)
	if rec.Error != "" {
		t.Fatalf("unexpected error field %q", rec.Error)
	}
	if len(trace.Records()) != 1 {
		t.Fatalf("trace has %d records", len(trace.Records()))
	}
}

func TestCallGivesUpAfterMaxRetries(t *testing.T) {
	srv, calls := fakeServer(t, 100, 500)
	c := NewClient("test-key", "", option.WithBaseURL(srv.URL))
	var trace Trace
	_, rec, err := c.Call(context.Background(), testRequest(), &trace)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != MaxRetries+1 || rec.Attempts != MaxRetries+1 {
		t.Fatalf("calls %d attempts %d", calls.Load(), rec.Attempts)
	}
	if rec.Error == "" || len(trace.Records()) != 1 {
		t.Fatalf("record %+v", rec)
	}
}

func TestCallNoRetryOn400(t *testing.T) {
	srv, calls := fakeServer(t, 100, 400)
	c := NewClient("test-key", "", option.WithBaseURL(srv.URL))
	_, rec, err := c.Call(context.Background(), testRequest(), nil)
	if err == nil || calls.Load() != 1 || rec.Attempts != 1 {
		t.Fatalf("err %v calls %d attempts %d", err, calls.Load(), rec.Attempts)
	}
}

func TestTraceJSONLRoundTrip(t *testing.T) {
	var trace Trace
	trace.Append(CallRecord{Model: ModelSonnet5, Tier: TierMid, Purpose: "a", Usage: Usage{InputTokens: 10, OutputTokens: 2}, CostUSD: 0.00004, Attempts: 1})
	trace.Append(CallRecord{Model: ModelOpus55, Tier: TierTop, Purpose: "b", Usage: Usage{CacheReadInputTokens: 100}, CostUSD: 0.00002, Attempts: 2})
	path := filepath.Join(t.TempDir(), "traces", "run.jsonl")
	if err := WriteJSONL(path, trace.Records()); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Tier != TierTop || got[1].Usage.CacheReadInputTokens != 100 || got[1].Attempts != 2 {
		t.Fatalf("round trip %+v", got)
	}
	approx(t, trace.TotalCost(), 0.00006)
	if trace.TotalUsage().PromptTokens() != 110 {
		t.Fatalf("total usage %+v", trace.TotalUsage())
	}
}
