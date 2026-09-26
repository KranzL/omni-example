package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/llm"
)

func completion(finish string, toolCalls string) string {
	return fmt.Sprintf(`{"id":"chatcmpl-x","model":"qwen3-5-9b","choices":[{"finish_reason":%q,"message":{"role":"assistant","content":null,"tool_calls":[%s]}}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":60}}}`, finish, toolCalls)
}

func toolCall(id, name string, args map[string]string) string {
	raw, _ := json.Marshal(args)
	quoted, _ := json.Marshal(string(raw))
	return fmt.Sprintf(`{"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}`, id, name, quoted)
}

func TestOpenAIProviderRoundTrip(t *testing.T) {
	responses := []string{
		completion("tool_calls", toolCall("call_sql", ToolRunSQL, map[string]string{"sql": "SELECT COUNT(*) FROM users"})),
		completion("tool_calls", toolCall("call_submit", ToolSubmitAnswer, map[string]string{"answer": "42", "sql": "SELECT COUNT(*) FROM users", "confidence": "high"})),
	}
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("bad body: %v", err)
		}
		bodies = append(bodies, body)
		if len(bodies) > len(responses) {
			w.WriteHeader(400)
			return
		}
		io.WriteString(w, responses[len(bodies)-1])
	}))
	t.Cleanup(srv.Close)
	client := llm.NewOpenAIClient(llm.ProviderVenice, srv.URL, "test-key", llm.VeniceDefaultSettings(), nil)
	client.Backoff = time.Millisecond
	q := &fakeDB{}
	a := New(client, q, "SEMANTIC LAYER TEXT")

	res, err := a.Run(context.Background(), "How many users?", llm.TierCheap)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Submitted || res.Answer != "42" || res.Turns != 2 || len(q.calls) != 1 {
		t.Fatalf("result %+v", res)
	}
	if res.Usage.InputTokens != 80 || res.Usage.CacheReadInputTokens != 120 || res.CostUSD <= 0 {
		t.Fatalf("usage %+v cost %f", res.Usage, res.CostUSD)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests %d", len(bodies))
	}
	msgs := bodies[1]["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("second request messages %d: %v", len(msgs), msgs)
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != Instructions+"\n\nSEMANTIC LAYER TEXT" {
		t.Fatalf("system %v", sys)
	}
	asst := msgs[2].(map[string]any)
	calls, _ := asst["tool_calls"].([]any)
	if asst["role"] != "assistant" || len(calls) != 1 {
		t.Fatalf("assistant %v", asst)
	}
	call := calls[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if call["id"] != "call_sql" || fn["name"] != ToolRunSQL || fn["arguments"] != `{"sql":"SELECT COUNT(*) FROM users"}` {
		t.Fatalf("tool call %v", call)
	}
	tool := msgs[3].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_sql" || tool["content"] != "rows: 1\ncapped: false\n\ncount\n42\n" {
		t.Fatalf("tool message %q", tool)
	}
	if len(bodies[1]["tools"].([]any)) != 2 || bodies[1]["tool_choice"] != "auto" {
		t.Fatalf("tools %v choice %v", bodies[1]["tools"], bodies[1]["tool_choice"])
	}
}
