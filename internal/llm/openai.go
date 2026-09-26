package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

const OpenAIMaxAttempts = 4

type OpenAISettings struct {
	Model           string
	MaxTokens       int64
	ReasoningEffort string
	DisableThinking bool
}

type OpenAIClient struct {
	name     string
	baseURL  string
	apiKey   string
	http     *http.Client
	settings map[Tier]OpenAISettings
	extra    map[string]any
	Backoff  time.Duration
	capture  *Capture
}

func (c *OpenAIClient) SetCapture(cap *Capture) {
	c.capture = cap
}

func NewOpenAIClient(name, baseURL, apiKey string, settings map[Tier]OpenAISettings, extra map[string]any) *OpenAIClient {
	return &OpenAIClient{
		name:     name,
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   apiKey,
		http:     &http.Client{Timeout: 5 * time.Minute},
		settings: settings,
		extra:    extra,
		Backoff:  time.Second,
	}
}

func (c *OpenAIClient) Name() string {
	return c.name
}

func (c *OpenAIClient) Settings(t Tier) OpenAISettings {
	return c.settings[t]
}

type oaiFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Arguments   *string         `json:"arguments,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    *string       `json:"content"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiRequest struct {
	Model            string         `json:"model"`
	Messages         []oaiMessage   `json:"messages"`
	Tools            []oaiTool      `json:"tools,omitempty"`
	ToolChoice       string         `json:"tool_choice,omitempty"`
	MaxTokens        int64          `json:"max_tokens,omitempty"`
	ReasoningEffort  string         `json:"reasoning_effort,omitempty"`
	VeniceParameters map[string]any `json:"venice_parameters,omitempty"`
}

type oaiUsage struct {
	PromptTokens        int64 `json:"prompt_tokens"`
	CompletionTokens    int64 `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

type oaiResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content   *string       `json:"content"`
			ToolCalls []oaiToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage oaiUsage `json:"usage"`
}

type anthBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

type anthMessage struct {
	Role    string      `json:"role"`
	Content []anthBlock `json:"content"`
}

type anthTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

func strPtr(s string) *string {
	return &s
}

func TranslateRequest(req Request, s OpenAISettings) (oaiRequest, error) {
	out := oaiRequest{Model: s.Model, MaxTokens: s.MaxTokens, ReasoningEffort: s.ReasoningEffort}
	var system []string
	for _, b := range req.System {
		if b.Text != "" {
			system = append(system, b.Text)
		}
	}
	if len(system) > 0 {
		out.Messages = append(out.Messages, oaiMessage{Role: "system", Content: strPtr(strings.Join(system, "\n\n"))})
	}
	for _, mp := range req.Messages {
		raw, err := json.Marshal(mp)
		if err != nil {
			return out, err
		}
		var m anthMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return out, err
		}
		msgs, err := translateMessage(m)
		if err != nil {
			return out, err
		}
		out.Messages = append(out.Messages, msgs...)
	}
	for _, tu := range req.Tools {
		raw, err := json.Marshal(tu)
		if err != nil {
			return out, err
		}
		var t anthTool
		if err := json.Unmarshal(raw, &t); err != nil {
			return out, err
		}
		out.Tools = append(out.Tools, oaiTool{Type: "function", Function: oaiFunction{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		}})
	}
	if len(out.Tools) > 0 {
		out.ToolChoice = "auto"
	}
	return out, nil
}

func translateMessage(m anthMessage) ([]oaiMessage, error) {
	var out []oaiMessage
	var texts []string
	var calls []oaiToolCall
	for _, b := range m.Content {
		switch b.Type {
		case "text":
			texts = append(texts, b.Text)
		case "tool_use":
			args := toolArguments(b.Input)
			calls = append(calls, oaiToolCall{ID: b.ID, Type: "function", Function: oaiFunction{Name: b.Name, Arguments: &args}})
		case "tool_result":
			content, err := toolResultText(b.Content)
			if err != nil {
				return nil, err
			}
			out = append(out, oaiMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: strPtr(content)})
		case "thinking", "redacted_thinking":
		default:
			return nil, fmt.Errorf("unsupported content block %q", b.Type)
		}
	}
	if m.Role == "assistant" {
		msg := oaiMessage{Role: "assistant", ToolCalls: calls}
		if len(texts) > 0 {
			msg.Content = strPtr(strings.Join(texts, "\n"))
		}
		if msg.Content != nil || len(calls) > 0 {
			out = append(out, msg)
		}
		return out, nil
	}
	if len(texts) > 0 {
		out = append(out, oaiMessage{Role: "user", Content: strPtr(strings.Join(texts, "\n"))})
	}
	return out, nil
}

func toolArguments(input json.RawMessage) string {
	if len(input) == 0 {
		return "{}"
	}
	var s string
	if json.Unmarshal(input, &s) == nil {
		return s
	}
	return string(input)
}

func toolResultText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []anthBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", fmt.Errorf("tool_result content: %w", err)
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func TranslateResponse(resp oaiResponse, model string) (*anthropic.Message, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("response %s has no choices", resp.ID)
	}
	choice := resp.Choices[0]
	blocks := []anthBlock{}
	if choice.Message.Content != nil && strings.TrimSpace(*choice.Message.Content) != "" {
		blocks = append(blocks, anthBlock{Type: "text", Text: *choice.Message.Content})
	}
	for i, tc := range choice.Message.ToolCalls {
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		args := ""
		if tc.Function.Arguments != nil {
			args = *tc.Function.Arguments
		}
		input := json.RawMessage(args)
		if strings.TrimSpace(args) == "" {
			input = json.RawMessage("{}")
		} else if !json.Valid(input) {
			quoted, _ := json.Marshal(args)
			input = quoted
		}
		blocks = append(blocks, anthBlock{Type: "tool_use", ID: id, Name: tc.Function.Name, Input: input})
	}
	stop := "end_turn"
	switch choice.FinishReason {
	case "tool_calls", "function_call":
		stop = "tool_use"
	case "length":
		stop = "max_tokens"
	}
	if stop == "end_turn" && len(choice.Message.ToolCalls) > 0 {
		stop = "tool_use"
	}
	cached := resp.Usage.PromptTokensDetails.CachedTokens
	body := map[string]any{
		"id":          resp.ID,
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     blocks,
		"stop_reason": stop,
		"usage": map[string]any{
			"input_tokens":                resp.Usage.PromptTokens - cached,
			"output_tokens":               resp.Usage.CompletionTokens,
			"cache_read_input_tokens":     cached,
			"cache_creation_input_tokens": 0,
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var msg anthropic.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("status %d: %s", e.status, e.body)
}

type afterSuccessError struct {
	err error
}

func (e *afterSuccessError) Error() string {
	return e.err.Error() + " (after status 200, not retried: the request may already be billed)"
}

func (e *afterSuccessError) Unwrap() error {
	return e.err
}

func retryable(err error) bool {
	var ae *afterSuccessError
	if errors.As(err, &ae) {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.status == http.StatusTooManyRequests || se.status >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (c *OpenAIClient) Call(ctx context.Context, req Request, trace *Trace) (*anthropic.Message, CallRecord, error) {
	s, ok := c.settings[req.Tier]
	if !ok {
		return nil, CallRecord{}, fmt.Errorf("unknown tier %q", req.Tier)
	}
	start := time.Now()
	rec := CallRecord{Time: start.UTC(), Provider: c.name, Model: s.Model, Tier: req.Tier, Purpose: req.Purpose}
	finish := func(msg *anthropic.Message, err error) (*anthropic.Message, CallRecord, error) {
		rec.LatencyMS = time.Since(start).Milliseconds()
		if err != nil {
			rec.Error = err.Error()
		}
		if trace != nil {
			trace.Append(rec)
		}
		return msg, rec, err
	}
	body, err := TranslateRequest(req, s)
	if err != nil {
		return finish(nil, err)
	}
	payload, err := c.payload(body, s)
	if err != nil {
		return finish(nil, err)
	}
	if c.capture != nil {
		c.capture.Save(payload)
	}
	var resp oaiResponse
	for attempt := 1; ; attempt++ {
		rec.Attempts = attempt
		var reqID string
		resp, reqID, err = c.post(ctx, payload)
		rec.RequestID = reqID
		if err == nil || attempt >= OpenAIMaxAttempts || !retryable(err) {
			break
		}
		select {
		case <-ctx.Done():
			return finish(nil, ctx.Err())
		case <-time.After(c.Backoff << (attempt - 1)):
		}
	}
	if err != nil {
		return finish(nil, err)
	}
	msg, err := TranslateResponse(resp, s.Model)
	if err != nil {
		return finish(nil, err)
	}
	rec.MessageID = resp.ID
	rec.StopReason = string(msg.StopReason)
	rec.Usage = UsageFrom(msg.Usage)
	cost, costErr := Cost(s.Model, rec.Usage)
	rec.CostUSD = cost
	return finish(msg, costErr)
}

func (c *OpenAIClient) payload(body oaiRequest, s OpenAISettings) ([]byte, error) {
	if len(c.extra) > 0 || s.DisableThinking {
		body.VeniceParameters = map[string]any{}
		for k, v := range c.extra {
			body.VeniceParameters[k] = v
		}
		if s.DisableThinking {
			body.VeniceParameters["disable_thinking"] = true
		}
	}
	return json.Marshal(body)
}

func (c *OpenAIClient) post(ctx context.Context, payload []byte) (oaiResponse, string, error) {
	var out oaiResponse
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return out, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return out, "", err
	}
	defer resp.Body.Close()
	reqID := resp.Header.Get("x-request-id")
	if reqID == "" {
		reqID = resp.Header.Get("cf-ray")
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		if resp.StatusCode == http.StatusOK {
			return out, reqID, &afterSuccessError{err: err}
		}
		return out, reqID, err
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return out, reqID, &statusError{status: resp.StatusCode, body: msg}
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, reqID, &afterSuccessError{err: fmt.Errorf("decode response: %w", err)}
	}
	return out, reqID, nil
}
