package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func toolParam() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{{OfTool: &anthropic.ToolParam{
		Name:        "run_sql",
		Description: anthropic.String("Run SQL."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{"sql": map[string]any{"type": "string"}},
			Required:   []string{"sql"},
		},
	}}}
}

func TestTranslateRequest(t *testing.T) {
	req := Request{
		Tier: TierCheap,
		System: []anthropic.TextBlockParam{
			{Text: "INSTRUCTIONS"},
			{Text: "SEMANTIC", CacheControl: anthropic.NewCacheControlEphemeralParam()},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("How many users?")),
			anthropic.NewAssistantMessage(
				anthropic.NewTextBlock("Checking."),
				anthropic.NewToolUseBlock("call_1", map[string]any{"sql": "SELECT 1"}, "run_sql"),
			),
			anthropic.NewUserMessage(
				anthropic.NewToolResultBlock("call_1", "rows: 1", false),
				anthropic.NewTextBlock("Submit now."),
			),
		},
		Tools: toolParam(),
	}
	body, err := TranslateRequest(req, OpenAISettings{Model: VeniceQwen359B, MaxTokens: 4096, ReasoningEffort: "none"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	var got map[string]any
	json.Unmarshal(raw, &got)
	if got["model"] != VeniceQwen359B || got["max_tokens"] != float64(4096) || got["reasoning_effort"] != "none" || got["tool_choice"] != "auto" {
		t.Fatalf("top level %v", got)
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("messages %d: %s", len(msgs), raw)
	}
	roles := []string{"system", "user", "assistant", "tool", "user"}
	for i, r := range roles {
		if msgs[i].(map[string]any)["role"] != r {
			t.Fatalf("message %d role %v want %s", i, msgs[i], r)
		}
	}
	if msgs[0].(map[string]any)["content"] != "INSTRUCTIONS\n\nSEMANTIC" {
		t.Fatalf("system %v", msgs[0])
	}
	if strings.Contains(string(raw), "cache_control") {
		t.Fatalf("cache markers leaked: %s", raw)
	}
	asst := msgs[2].(map[string]any)
	calls := asst["tool_calls"].([]any)
	call := calls[0].(map[string]any)
	fn := call["function"].(map[string]any)
	if asst["content"] != "Checking." || call["id"] != "call_1" || call["type"] != "function" || fn["name"] != "run_sql" || fn["arguments"] != `{"sql":"SELECT 1"}` {
		t.Fatalf("assistant %v", asst)
	}
	tool := msgs[3].(map[string]any)
	if tool["tool_call_id"] != "call_1" || tool["content"] != "rows: 1" {
		t.Fatalf("tool %v", tool)
	}
	if msgs[4].(map[string]any)["content"] != "Submit now." {
		t.Fatalf("trailing user %v", msgs[4])
	}
	tools := got["tools"].([]any)
	f := tools[0].(map[string]any)["function"].(map[string]any)
	params := f["parameters"].(map[string]any)
	if f["name"] != "run_sql" || f["description"] != "Run SQL." || params["type"] != "object" || params["properties"] == nil {
		t.Fatalf("tool %v", tools[0])
	}
}

func TestTranslateResponse(t *testing.T) {
	var resp oaiResponse
	err := json.Unmarshal([]byte(`{"id":"chatcmpl-1","model":"qwen3-5-9b","choices":[{"finish_reason":"tool_calls","message":{"role":"assistant","content":"Let me look.","tool_calls":[{"id":"call_a","type":"function","function":{"name":"run_sql","arguments":"{\"sql\":\"SELECT 2\"}"}},{"id":"","type":"function","function":{"name":"run_sql","arguments":"not json"}}]}}],"usage":{"prompt_tokens":1000,"completion_tokens":40,"prompt_tokens_details":{"cached_tokens":600}}}`), &resp)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := TranslateResponse(resp, VeniceQwen359B)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != anthropic.StopReasonToolUse || len(msg.Content) != 3 {
		t.Fatalf("message %+v", msg)
	}
	if msg.Content[0].Type != "text" || msg.Content[0].Text != "Let me look." {
		t.Fatalf("text block %+v", msg.Content[0])
	}
	tu := msg.Content[1].AsToolUse()
	if tu.ID != "call_a" || tu.Name != "run_sql" || string(tu.Input) != `{"sql":"SELECT 2"}` {
		t.Fatalf("tool use %+v", tu)
	}
	bad := msg.Content[2].AsToolUse()
	if bad.ID != "call_1" || string(bad.Input) != `"not json"` {
		t.Fatalf("bad tool use %+v", bad)
	}
	if u := UsageFrom(msg.Usage); u.InputTokens != 400 || u.CacheReadInputTokens != 600 || u.OutputTokens != 40 || u.CacheCreationInputTokens != 0 {
		t.Fatalf("usage %+v", u)
	}
	param := msg.ToParam()
	if param.Role != anthropic.MessageParamRoleAssistant || param.Content[1].OfToolUse == nil || param.Content[1].OfToolUse.ID != "call_a" {
		t.Fatalf("param %+v", param)
	}

	for finish, want := range map[string]anthropic.StopReason{"stop": anthropic.StopReasonEndTurn, "length": anthropic.StopReasonMaxTokens, "tool_calls": anthropic.StopReasonToolUse} {
		r := oaiResponse{ID: "x"}
		r.Choices = make([]struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   *string       `json:"content"`
				ToolCalls []oaiToolCall `json:"tool_calls"`
			} `json:"message"`
		}, 1)
		r.Choices[0].FinishReason = finish
		r.Choices[0].Message.Content = strPtr("done")
		m, err := TranslateResponse(r, VeniceQwen359B)
		if err != nil || m.StopReason != want {
			t.Errorf("%s: stop %s err %v", finish, m.StopReason, err)
		}
	}
}

func TestVeniceCost(t *testing.T) {
	u := Usage{InputTokens: 400_000, CacheReadInputTokens: 600_000, OutputTokens: 100_000}
	cases := map[string]float64{
		VeniceQwen359B:        0.4*0.10 + 0.6*0.10 + 0.1*0.15,
		VeniceDeepseekV4Flash: 0.4*0.138 + 0.6*0.028 + 0.1*0.275,
		VeniceQwen37Plus:      0.4*0.50 + 0.6*0.05 + 0.1*2.00,
		VeniceEmbedBgeM3:      0.4 * 0.15,
	}
	for model, want := range cases {
		got, err := Cost(model, u)
		if err != nil {
			t.Fatal(err)
		}
		if d := got - want; d > 1e-12 || d < -1e-12 {
			t.Errorf("%s cost %.8f want %.8f", model, got, want)
		}
	}
	for _, s := range VeniceDefaultSettings() {
		if _, ok := PriceFor(s.Model); !ok {
			t.Errorf("no price for default %s", s.Model)
		}
	}
}

func TestVeniceSettingsOverride(t *testing.T) {
	s := VeniceSettings(map[Tier]string{TierCheap: "other-model", TierMid: ""})
	if s[TierCheap].Model != "other-model" || s[TierCheap].ReasoningEffort != "" || !s[TierCheap].DisableThinking {
		t.Fatalf("cheap %+v", s[TierCheap])
	}
	if s[TierMid].Model != VeniceDeepseekV4Flash || s[TierMid].ReasoningEffort != "low" || s[TierMid].MaxTokens != 16000 {
		t.Fatalf("mid %+v", s[TierMid])
	}
	if s[TierTop].Model != VeniceQwen37Plus || s[TierTop].MaxTokens != 16000 {
		t.Fatalf("top %+v", s[TierTop])
	}
}

const fakeCompletion = `{"id":"chatcmpl-ok","model":"qwen3-5-9b","choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"4"}}],"usage":{"prompt_tokens":20,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0}}}`

func fakeOpenAI(t *testing.T, failures int32, status int) (*httptest.Server, *atomic.Int32, *[]map[string]any) {
	t.Helper()
	var calls atomic.Int32
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("path %s auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		data, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(data, &body)
		bodies = append(bodies, body)
		n := calls.Add(1)
		w.Header().Set("x-request-id", "vreq")
		if n <= failures {
			w.WriteHeader(status)
			io.WriteString(w, `{"error":"busy"}`)
			return
		}
		io.WriteString(w, fakeCompletion)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &bodies
}

func testOpenAIClient(url string) *OpenAIClient {
	c := NewOpenAIClient(ProviderVenice, url, "test-key", VeniceDefaultSettings(), map[string]any{"include_venice_system_prompt": false})
	c.Backoff = time.Millisecond
	return c
}

func TestOpenAICallRetriesAndRecords(t *testing.T) {
	srv, calls, bodies := fakeOpenAI(t, 2, 429)
	var trace Trace
	msg, rec, err := testOpenAIClient(srv.URL).Call(context.Background(), testRequest(), &trace)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content[0].Text != "4" || calls.Load() != 3 || rec.Attempts != 3 {
		t.Fatalf("calls %d record %+v", calls.Load(), rec)
	}
	if rec.Provider != ProviderVenice || rec.Model != VeniceQwen359B || rec.RequestID != "vreq" || rec.StopReason != "end_turn" || rec.MessageID != "chatcmpl-ok" {
		t.Fatalf("record %+v", rec)
	}
	approx(t, rec.CostUSD, (20*0.10+5*0.15)/1e6)
	vp := (*bodies)[0]["venice_parameters"].(map[string]any)
	if vp["include_venice_system_prompt"] != false {
		t.Fatalf("venice_parameters %v", vp)
	}
	if len(trace.Records()) != 1 {
		t.Fatalf("trace %d", len(trace.Records()))
	}
}

func TestOpenAICallGivesUp(t *testing.T) {
	srv, calls, _ := fakeOpenAI(t, 100, 503)
	var trace Trace
	_, rec, err := testOpenAIClient(srv.URL).Call(context.Background(), testRequest(), &trace)
	if err == nil || calls.Load() != OpenAIMaxAttempts || rec.Attempts != OpenAIMaxAttempts || rec.Error == "" {
		t.Fatalf("err %v calls %d record %+v", err, calls.Load(), rec)
	}
}

func TestOpenAICallNoRetryOn400(t *testing.T) {
	srv, calls, _ := fakeOpenAI(t, 100, 400)
	_, rec, err := testOpenAIClient(srv.URL).Call(context.Background(), testRequest(), nil)
	if err == nil || calls.Load() != 1 || rec.Attempts != 1 {
		t.Fatalf("err %v calls %d attempts %d", err, calls.Load(), rec.Attempts)
	}
}

func TestIntegrationVeniceCheapToolCall(t *testing.T) {
	if os.Getenv("OMNI_INTEGRATION") != "1" {
		t.Skip("OMNI_INTEGRATION=1 is required for paid integration tests")
	}
	key := os.Getenv("VENICE_API_KEY")
	if key == "" {
		t.Skip("VENICE_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := NewVeniceClient(key, nil)
	msg, rec, err := c.Call(ctx, Request{
		Tier:     TierCheap,
		Purpose:  "integration",
		System:   []anthropic.TextBlockParam{{Text: "You answer questions by calling run_sql."}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("Count the rows in the users table."))},
		Tools:    toolParam(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("record %+v", rec)
	if msg.StopReason != anthropic.StopReasonToolUse || rec.CostUSD <= 0 || rec.Usage.PromptTokens() == 0 {
		t.Fatalf("stop %s record %+v", msg.StopReason, rec)
	}
}

func TestOpenAICallNoRetryOnBadBodyAfter200(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		io.WriteString(w, `{"id":"chatcmpl-cut","choices":[`)
	}))
	t.Cleanup(srv.Close)
	_, rec, err := testOpenAIClient(srv.URL).Call(context.Background(), testRequest(), nil)
	if err == nil || calls.Load() != 1 || rec.Attempts != 1 {
		t.Fatalf("err %v calls %d attempts %d", err, calls.Load(), rec.Attempts)
	}
}
