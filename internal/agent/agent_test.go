package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/llm"
)

type fakeAPI struct {
	mu        sync.Mutex
	responses []string
	bodies    []map[string]any
	raw       [][]byte
}

func (f *fakeAPI) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		var parsed map[string]any
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		f.bodies = append(f.bodies, parsed)
		f.raw = append(f.raw, body)
		n := len(f.bodies)
		w.Header().Set("content-type", "application/json")
		w.Header().Set("request-id", fmt.Sprintf("req_%d", n))
		if n > len(f.responses) {
			w.WriteHeader(500)
			io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"no more scripted responses"}}`)
			return
		}
		io.WriteString(w, f.responses[n-1])
	}
}

func message(stop string, cacheRead int, blocks ...string) string {
	return fmt.Sprintf(`{"id":"msg_x","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[%s],"stop_reason":%q,"usage":{"input_tokens":50,"output_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d}}`,
		strings.Join(blocks, ","), stop, cacheRead)
}

func toolUse(id, name string, input map[string]string) string {
	raw, _ := json.Marshal(input)
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":%s}`, id, name, raw)
}

func text(s string) string {
	return fmt.Sprintf(`{"type":"text","text":%q}`, s)
}

type fakeDB struct {
	calls []string
}

func (f *fakeDB) Query(ctx context.Context, sql string, maxRows int) (db.Result, error) {
	f.calls = append(f.calls, sql)
	if strings.Contains(sql, "bogus") {
		return db.Result{}, fmt.Errorf(`ERROR: column "bogus" does not exist (SQLSTATE 42703)`)
	}
	return db.Result{Columns: []string{"count"}, Rows: [][]string{{"42"}}}, nil
}

func newTestAgent(t *testing.T, responses ...string) (*Agent, *fakeAPI, *fakeDB) {
	t.Helper()
	api := &fakeAPI{responses: responses}
	srv := httptest.NewServer(api.handler(t))
	t.Cleanup(srv.Close)
	client := llm.NewClient("test-key", "", option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	q := &fakeDB{}
	return New(client, q, "SEMANTIC LAYER TEXT"), api, q
}

func TestRunHappyPath(t *testing.T) {
	a, api, q := newTestAgent(t,
		message("tool_use", 0, text("Checking."), toolUse("tu_1", ToolRunSQL, map[string]string{"sql": "SELECT bogus FROM users"})),
		message("tool_use", 100, toolUse("tu_2", ToolRunSQL, map[string]string{"sql": "SELECT COUNT(*) FROM users"})),
		message("tool_use", 200, toolUse("tu_3", ToolSubmitAnswer, map[string]string{"answer": " 42 ", "sql": "SELECT COUNT(*) FROM users", "confidence": "high"})),
	)
	res, err := a.Run(context.Background(), "How many users?", llm.TierCheap)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Submitted || res.Nudged || res.Answer != "42" || res.Confidence != "high" || res.SQL != "SELECT COUNT(*) FROM users" {
		t.Fatalf("result %+v", res)
	}
	if res.Turns != 3 || res.SQLErrors != 1 || len(res.Steps) != 2 || len(q.calls) != 2 || len(res.Records) != 3 {
		t.Fatalf("turns %d errors %d steps %d calls %d records %d", res.Turns, res.SQLErrors, len(res.Steps), len(q.calls), len(res.Records))
	}
	if res.CacheRead != 300 || res.Usage.InputTokens != 150 || res.CostUSD <= 0 {
		t.Fatalf("usage %+v cost %f", res.Usage, res.CostUSD)
	}

	second := api.bodies[1]["messages"].([]any)
	results := second[2].(map[string]any)["content"].([]any)
	tr := results[0].(map[string]any)
	if tr["type"] != "tool_result" || tr["is_error"] != true || !strings.Contains(fmt.Sprint(tr["content"]), "bogus") {
		t.Fatalf("error tool result %v", tr)
	}
	third := api.bodies[2]["messages"].([]any)
	ok := third[4].(map[string]any)["content"].([]any)[0].(map[string]any)
	if !strings.Contains(fmt.Sprint(ok["content"]), "rows: 1\ncapped: false\n\ncount\n42") {
		t.Fatalf("ok tool result %v", ok)
	}
}

func TestRequestShapeIsCacheable(t *testing.T) {
	a, api, _ := newTestAgent(t,
		message("tool_use", 0, toolUse("tu_1", ToolRunSQL, map[string]string{"sql": "SELECT 1"})),
		message("tool_use", 0, toolUse("tu_2", ToolSubmitAnswer, map[string]string{"answer": "1", "sql": "SELECT 1", "confidence": "high"})),
	)
	if _, err := a.Run(context.Background(), "Q1", llm.TierCheap); err != nil {
		t.Fatal(err)
	}
	b := api.bodies[0]
	sys := b["system"].([]any)
	if len(sys) != 2 {
		t.Fatalf("system blocks %d", len(sys))
	}
	if sys[0].(map[string]any)["text"] != Instructions || sys[0].(map[string]any)["cache_control"] != nil {
		t.Fatalf("first system block %v", sys[0])
	}
	last := sys[1].(map[string]any)
	if last["text"] != "SEMANTIC LAYER TEXT" || last["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Fatalf("second system block %v", last)
	}
	tools := b["tools"].([]any)
	if len(tools) != 2 || tools[0].(map[string]any)["name"] != ToolRunSQL || tools[1].(map[string]any)["name"] != ToolSubmitAnswer {
		t.Fatalf("tools %v", tools)
	}
	submit := tools[1].(map[string]any)
	schema := submit["input_schema"].(map[string]any)
	if submit["strict"] != true || schema["additionalProperties"] != false {
		t.Fatalf("submit_answer schema %v", submit)
	}
	msgs := b["messages"].([]any)
	first := msgs[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if first["text"] != "Q1" {
		t.Fatalf("question should be the first user message: %v", first)
	}

	breakpoints := func(body map[string]any) int {
		n := 0
		for _, m := range body["messages"].([]any) {
			for _, c := range m.(map[string]any)["content"].([]any) {
				if c.(map[string]any)["cache_control"] != nil {
					n++
				}
			}
		}
		return n
	}
	if breakpoints(api.bodies[0]) != 1 || breakpoints(api.bodies[1]) != 1 {
		t.Fatalf("message breakpoints %d %d", breakpoints(api.bodies[0]), breakpoints(api.bodies[1]))
	}

	a2, api2, _ := newTestAgent(t,
		message("tool_use", 0, toolUse("tu_9", ToolSubmitAnswer, map[string]string{"answer": "2", "sql": "SELECT 2", "confidence": "low"})),
	)
	if _, err := a2.Run(context.Background(), "A different question", llm.TierCheap); err != nil {
		t.Fatal(err)
	}
	var one, two map[string]any
	json.Unmarshal(api.raw[0], &one)
	json.Unmarshal(api2.raw[0], &two)
	for _, key := range []string{"system", "tools", "model", "tool_choice", "thinking"} {
		x, _ := json.Marshal(one[key])
		y, _ := json.Marshal(two[key])
		if string(x) != string(y) {
			t.Fatalf("%s differs between runs:\n%s\n%s", key, x, y)
		}
	}
}

func stripCacheControl(v any) {
	switch n := v.(type) {
	case map[string]any:
		delete(n, "cache_control")
		for _, c := range n {
			stripCacheControl(c)
		}
	case []any:
		for _, c := range n {
			stripCacheControl(c)
		}
	}
}

func TestNoCacheOmitsOnlyMarkers(t *testing.T) {
	responses := []string{
		message("tool_use", 0, toolUse("tu_1", ToolRunSQL, map[string]string{"sql": "SELECT 1"})),
		message("tool_use", 0, toolUse("tu_2", ToolSubmitAnswer, map[string]string{"answer": "1", "sql": "SELECT 1", "confidence": "high"})),
	}
	cached, cachedAPI, _ := newTestAgent(t, responses...)
	if _, err := cached.Run(context.Background(), "Q1", llm.TierCheap); err != nil {
		t.Fatal(err)
	}
	plain, plainAPI, _ := newTestAgent(t, responses...)
	plain = NewNoCache(plain.LLM, plain.DB, "SEMANTIC LAYER TEXT")
	if _, err := plain.Run(context.Background(), "Q1", llm.TierCheap); err != nil {
		t.Fatal(err)
	}
	if len(cachedAPI.raw) != len(plainAPI.raw) {
		t.Fatalf("turns %d vs %d", len(cachedAPI.raw), len(plainAPI.raw))
	}
	for i := range cachedAPI.raw {
		if !strings.Contains(string(cachedAPI.raw[i]), "cache_control") {
			t.Fatalf("cached turn %d has no marker", i)
		}
		if strings.Contains(string(plainAPI.raw[i]), "cache_control") {
			t.Fatalf("uncached turn %d still has a marker:\n%s", i, plainAPI.raw[i])
		}
		var want, got map[string]any
		json.Unmarshal(cachedAPI.raw[i], &want)
		json.Unmarshal(plainAPI.raw[i], &got)
		stripCacheControl(want)
		x, _ := json.Marshal(want)
		y, _ := json.Marshal(got)
		if string(x) != string(y) {
			t.Fatalf("turn %d differs beyond cache_control:\n%s\n%s", i, x, y)
		}
	}
}

func TestRunNudgeThenSubmit(t *testing.T) {
	a, api, _ := newTestAgent(t,
		message("end_turn", 0, text("There are 42 users.")),
		message("tool_use", 0, toolUse("tu_1", ToolSubmitAnswer, map[string]string{"answer": "42", "sql": "SELECT 42", "confidence": "medium"})),
	)
	res, err := a.Run(context.Background(), "How many users?", llm.TierCheap)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Submitted || !res.Nudged || res.Answer != "42" || res.Turns != 2 {
		t.Fatalf("result %+v", res)
	}
	msgs := api.bodies[1]["messages"].([]any)
	nudge := msgs[len(msgs)-1].(map[string]any)["content"].([]any)
	if nudge[len(nudge)-1].(map[string]any)["text"] != NudgeText {
		t.Fatalf("nudge %v", nudge)
	}
}

func TestRunFailsAfterNudge(t *testing.T) {
	a, _, _ := newTestAgent(t,
		message("end_turn", 0, text("I think about 42.")),
		message("end_turn", 0, text("Still 42.")),
	)
	res, err := a.Run(context.Background(), "How many users?", llm.TierCheap)
	if err != nil {
		t.Fatal(err)
	}
	if res.Submitted || !res.Nudged || res.Answer != "Still 42." || res.Failure == "" || res.Turns != 2 {
		t.Fatalf("result %+v", res)
	}
}

func TestRunTurnCapThenNudge(t *testing.T) {
	var responses []string
	for i := 0; i < DefaultMaxTurns; i++ {
		responses = append(responses, message("tool_use", 0, toolUse(fmt.Sprintf("tu_%d", i), ToolRunSQL, map[string]string{"sql": "SELECT 1"})))
	}
	responses = append(responses, message("tool_use", 0, toolUse("tu_final", ToolSubmitAnswer, map[string]string{"answer": "1", "sql": "SELECT 1", "confidence": "low"})))
	a, api, q := newTestAgent(t, responses...)
	res, err := a.Run(context.Background(), "Loop forever", llm.TierCheap)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Submitted || !res.Nudged || res.Turns != DefaultMaxTurns+1 || len(q.calls) != DefaultMaxTurns {
		t.Fatalf("result submitted=%v nudged=%v turns=%d sql=%d", res.Submitted, res.Nudged, res.Turns, len(q.calls))
	}
	msgs := api.bodies[DefaultMaxTurns]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	content := last["content"].([]any)
	if last["role"] != "user" || content[0].(map[string]any)["type"] != "tool_result" || content[1].(map[string]any)["text"] != NudgeText {
		t.Fatalf("final user message %v", last)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].(map[string]any)["role"] == msgs[i-1].(map[string]any)["role"] {
			t.Fatalf("roles do not alternate at %d", i)
		}
	}
}

func TestRunAPIError(t *testing.T) {
	a, _, _ := newTestAgent(t)
	res, err := a.Run(context.Background(), "Q", llm.TierCheap)
	if err == nil || res.Turns != 1 || len(res.Records) != 1 {
		t.Fatalf("err %v result %+v", err, res)
	}
}

func TestRunIncludesEvidence(t *testing.T) {
	a, api, _ := newTestAgent(t,
		message("tool_use", 0, toolUse("tu_1", ToolSubmitAnswer, map[string]string{"answer": "42", "sql": "SELECT 42", "confidence": "high"})),
	)
	a.Evidence = "count means COUNT(*)"
	if _, err := a.Run(context.Background(), "How many?", llm.TierCheap); err != nil {
		t.Fatal(err)
	}
	first := api.bodies[0]["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if got := fmt.Sprint(first["text"]); got != "How many?\n\nContext\ncount means COUNT(*)" {
		t.Fatalf("user message = %q", got)
	}
}

func TestRunWithoutEvidenceUnchanged(t *testing.T) {
	a, api, _ := newTestAgent(t,
		message("tool_use", 0, toolUse("tu_1", ToolSubmitAnswer, map[string]string{"answer": "42", "sql": "SELECT 42", "confidence": "high"})),
	)
	if _, err := a.Run(context.Background(), "How many?", llm.TierCheap); err != nil {
		t.Fatal(err)
	}
	first := api.bodies[0]["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)
	if got := fmt.Sprint(first["text"]); got != "How many?" {
		t.Fatalf("user message = %q", got)
	}
}

func TestRunNudgesAfterEmptyAssistantTurn(t *testing.T) {
	a, api, _ := newTestAgent(t,
		message("end_turn", 0),
		message("tool_use", 0, toolUse("tu_1", ToolSubmitAnswer, map[string]string{"answer": "7", "sql": "SELECT 7", "confidence": "high"})),
	)
	res, err := a.Run(context.Background(), "Q", llm.TierCheap)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Submitted || !res.Nudged || res.Answer != "7" {
		t.Fatalf("result %+v", res)
	}
	for i, m := range api.bodies[1]["messages"].([]any) {
		content, _ := m.(map[string]any)["content"].([]any)
		if len(content) == 0 {
			t.Fatalf("message %d has empty content: %v", i, m)
		}
	}
}

func TestRunFailsOnUnpricedModel(t *testing.T) {
	unpriced := strings.Replace(message("tool_use", 0, toolUse("tu_1", ToolSubmitAnswer, map[string]string{"answer": "7", "sql": "SELECT 7", "confidence": "high"})), "claude-haiku-4-5-20251001", "claude-unknown-9", 1)
	a, _, _ := newTestAgent(t, unpriced)
	res, err := a.Run(context.Background(), "Q", llm.TierCheap)
	if err == nil || !strings.Contains(err.Error(), "claude-unknown-9") {
		t.Fatalf("err %v result %+v", err, res)
	}
}
