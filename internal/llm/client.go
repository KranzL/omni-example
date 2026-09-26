package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const MaxRetries = 4

type Client struct {
	api      anthropic.Client
	settings map[Tier]Settings
	capture  *Capture
}

func (c *Client) SetCapture(cap *Capture) {
	c.capture = cap
}

func NewClient(apiKey, workspaceID string, opts ...option.RequestOption) *Client {
	base := []option.RequestOption{option.WithMaxRetries(MaxRetries)}
	if apiKey != "" {
		base = append(base, option.WithAPIKey(apiKey))
	}
	if workspaceID != "" {
		base = append(base, option.WithHeader("anthropic-workspace-id", workspaceID))
	}
	return &Client{
		api:      anthropic.NewClient(append(base, opts...)...),
		settings: DefaultSettings(),
	}
}

func (c *Client) Settings(t Tier) Settings {
	return c.settings[t]
}

func (c *Client) SetSettings(t Tier, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	c.settings[t] = s
	return nil
}

type Request struct {
	Tier     Tier
	Purpose  string
	System   []anthropic.TextBlockParam
	Messages []anthropic.MessageParam
	Tools    []anthropic.ToolUnionParam
}

func (c *Client) Call(ctx context.Context, req Request, trace *Trace) (*anthropic.Message, CallRecord, error) {
	s, ok := c.settings[req.Tier]
	if !ok {
		return nil, CallRecord{}, fmt.Errorf("unknown tier %q", req.Tier)
	}
	params := anthropic.MessageNewParams{
		System:   req.System,
		Messages: req.Messages,
		Tools:    req.Tools,
	}
	s.Apply(&params)

	var attempts atomic.Int32
	var httpResp *http.Response
	countAttempts := option.WithMiddleware(func(r *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		attempts.Add(1)
		return next(r)
	})

	opts := []option.RequestOption{countAttempts, option.WithResponseInto(&httpResp)}
	if c.capture != nil {
		opts = append(opts, c.capture.Middleware())
	}
	start := time.Now()
	msg, err := c.api.Messages.New(ctx, params, opts...)
	rec := CallRecord{
		Time:      start.UTC(),
		Provider:  ProviderAnthropic,
		Model:     s.Model,
		Tier:      req.Tier,
		Purpose:   req.Purpose,
		LatencyMS: time.Since(start).Milliseconds(),
		Attempts:  int(attempts.Load()),
	}
	if httpResp != nil {
		rec.RequestID = httpResp.Header.Get("request-id")
	}
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) && apiErr.RequestID != "" {
			rec.RequestID = apiErr.RequestID
		}
		rec.Error = err.Error()
		if trace != nil {
			trace.Append(rec)
		}
		return nil, rec, err
	}
	rec.Model = string(msg.Model)
	rec.MessageID = msg.ID
	rec.StopReason = string(msg.StopReason)
	rec.Usage = UsageFrom(msg.Usage)
	cost, costErr := Cost(rec.Model, rec.Usage)
	rec.CostUSD = cost
	if costErr != nil {
		rec.Error = costErr.Error()
	}
	if trace != nil {
		trace.Append(rec)
	}
	return msg, rec, costErr
}
