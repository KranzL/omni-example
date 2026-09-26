package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	DefaultMaxTurns = 8
	DefaultMaxRows  = 100
)

type Querier interface {
	Query(ctx context.Context, sql string, maxRows int) (db.Result, error)
}

type PoolQuerier struct {
	Pool *pgxpool.Pool
}

func (q PoolQuerier) Query(ctx context.Context, sql string, maxRows int) (db.Result, error) {
	return db.Query(ctx, q.Pool, sql, maxRows)
}

type Caller interface {
	Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error)
}

type Agent struct {
	LLM      Caller
	DB       Querier
	System   []anthropic.TextBlockParam
	Tools    []anthropic.ToolUnionParam
	MaxTurns int
	MaxRows  int
	NoCache  bool
	Evidence string
}

func New(client Caller, q Querier, semanticText string) *Agent {
	return &Agent{
		LLM:      client,
		DB:       q,
		System:   SystemBlocks(semanticText),
		Tools:    Tools(),
		MaxTurns: DefaultMaxTurns,
		MaxRows:  DefaultMaxRows,
	}
}

func NewNoCache(client Caller, q Querier, semanticText string) *Agent {
	a := New(client, q, semanticText)
	a.System = SystemBlocksNoCache(semanticText)
	a.NoCache = true
	return a
}

func NewPrefix(client Caller, q Querier, semanticText string) *Agent {
	return &Agent{
		LLM:      client,
		DB:       q,
		System:   SystemBlocksWith(InstructionsFor(semanticText), semanticText),
		Tools:    Tools(),
		MaxTurns: DefaultMaxTurns,
		MaxRows:  DefaultMaxRows,
	}
}

func NewPrefixNoCache(client Caller, q Querier, semanticText string) *Agent {
	a := NewPrefix(client, q, semanticText)
	a.System = SystemBlocksNoCacheWith(InstructionsFor(semanticText), semanticText)
	a.NoCache = true
	return a
}

func NewPrefixSQLite(client Caller, q Querier, semanticText string) *Agent {
	return &Agent{
		LLM:      client,
		DB:       q,
		System:   SystemBlocksWith(InstructionsSQLiteFor(semanticText), semanticText),
		Tools:    ToolsSQLite(),
		MaxTurns: DefaultMaxTurns,
		MaxRows:  DefaultMaxRows,
	}
}

func NewPrefixSQLiteNoCache(client Caller, q Querier, semanticText string) *Agent {
	a := NewPrefixSQLite(client, q, semanticText)
	a.System = SystemBlocksNoCacheWith(InstructionsSQLiteFor(semanticText), semanticText)
	a.NoCache = true
	return a
}

type SQLStep struct {
	Turn       int    `json:"turn"`
	SQL        string `json:"sql"`
	Rows       int    `json:"rows"`
	Capped     bool   `json:"capped"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"-"`
}

type Result struct {
	Question      string           `json:"question"`
	Tier          llm.Tier         `json:"tier"`
	Answer        string           `json:"answer"`
	SQL           string           `json:"sql"`
	Confidence    string           `json:"confidence"`
	Submitted     bool             `json:"submitted"`
	Nudged        bool             `json:"nudged"`
	Failure       string           `json:"failure,omitempty"`
	Turns         int              `json:"turns"`
	SQLErrors     int              `json:"sql_errors"`
	Steps         []SQLStep        `json:"steps"`
	Records       []llm.CallRecord `json:"records"`
	Usage         llm.Usage        `json:"usage"`
	CostUSD       float64          `json:"cost_usd"`
	WallMS        int64            `json:"wall_ms"`
	CacheRead     int64            `json:"cache_read_input_tokens"`
	CacheCreation int64            `json:"cache_creation_input_tokens"`
}

type submission struct {
	Answer     string `json:"answer"`
	SQL        string `json:"sql"`
	Confidence string `json:"confidence"`
}

type run struct {
	a        *Agent
	tier     llm.Tier
	trace    llm.Trace
	messages []anthropic.MessageParam
	res      *Result
	lastText string
}

func (a *Agent) Run(ctx context.Context, question string, tier llm.Tier) (Result, error) {
	start := time.Now()
	text := question
	if strings.TrimSpace(a.Evidence) != "" {
		text += EvidenceBlock(a.Evidence)
	}
	r := &run{
		a:        a,
		tier:     tier,
		messages: []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(text))},
		res:      &Result{Question: question, Tier: tier},
	}
	err := r.loop(ctx)
	res := r.res
	res.Records = r.trace.Records()
	res.Usage = r.trace.TotalUsage()
	res.CostUSD = r.trace.TotalCost()
	res.CacheRead = res.Usage.CacheReadInputTokens
	res.CacheCreation = res.Usage.CacheCreationInputTokens
	res.WallMS = time.Since(start).Milliseconds()
	if err == nil && !res.Submitted {
		res.Answer = strings.TrimSpace(r.lastText)
		if res.Failure == "" {
			res.Failure = "no submit_answer call after nudge"
		}
	}
	return *res, err
}

func (r *run) loop(ctx context.Context) error {
	maxTurns := r.a.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	var pending []anthropic.ContentBlockParamUnion
	for {
		msg, err := r.call(ctx)
		if err != nil {
			return err
		}
		pending = r.handle(ctx, msg)
		if r.res.Submitted {
			return nil
		}
		if msg.StopReason != anthropic.StopReasonToolUse || len(pending) == 0 || r.res.Turns >= maxTurns {
			break
		}
		r.messages = append(r.messages, anthropic.NewUserMessage(pending...))
	}
	r.res.Nudged = true
	r.messages = append(r.messages, anthropic.NewUserMessage(append(pending, anthropic.NewTextBlock(NudgeText))...))
	msg, err := r.call(ctx)
	if err != nil {
		return err
	}
	r.handle(ctx, msg)
	return nil
}

func (r *run) call(ctx context.Context) (*anthropic.Message, error) {
	var cc *anthropic.CacheControlEphemeralParam
	if !r.a.NoCache {
		last := r.messages[len(r.messages)-1].Content
		cc = last[len(last)-1].GetCacheControl()
		if cc != nil {
			*cc = anthropic.NewCacheControlEphemeralParam()
		}
	}
	r.res.Turns++
	msg, _, err := r.a.LLM.Call(ctx, llm.Request{
		Tier:     r.tier,
		Purpose:  fmt.Sprintf("agent-turn-%d", r.res.Turns),
		System:   r.a.System,
		Messages: r.messages,
		Tools:    r.a.Tools,
	}, &r.trace)
	if cc != nil {
		*cc = anthropic.CacheControlEphemeralParam{}
	}
	if err != nil {
		return nil, fmt.Errorf("turn %d: %w", r.res.Turns, err)
	}
	if len(msg.Content) > 0 {
		r.messages = append(r.messages, msg.ToParam())
	}
	return msg, nil
}

func (r *run) handle(ctx context.Context, msg *anthropic.Message) []anthropic.ContentBlockParamUnion {
	var results []anthropic.ContentBlockParamUnion
	var texts []string
	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			texts = append(texts, block.Text)
		case "tool_use":
			tu := block.AsToolUse()
			content, isErr := r.tool(ctx, tu)
			results = append(results, anthropic.NewToolResultBlock(tu.ID, content, isErr))
		}
	}
	if len(texts) > 0 {
		r.lastText = strings.Join(texts, "\n")
	}
	return results
}

func (r *run) tool(ctx context.Context, tu anthropic.ToolUseBlock) (string, bool) {
	switch tu.Name {
	case ToolRunSQL:
		var in struct {
			SQL string `json:"sql"`
		}
		if err := json.Unmarshal(tu.Input, &in); err != nil {
			r.res.SQLErrors++
			return "invalid run_sql input: " + err.Error(), true
		}
		return r.runSQL(ctx, in.SQL)
	case ToolSubmitAnswer:
		var in submission
		if err := json.Unmarshal(tu.Input, &in); err != nil {
			return "invalid submit_answer input: " + err.Error(), true
		}
		if !r.res.Submitted {
			r.res.Submitted = true
			r.res.Answer = strings.TrimSpace(in.Answer)
			r.res.SQL = strings.TrimSpace(in.SQL)
			r.res.Confidence = in.Confidence
		}
		return "Answer recorded.", false
	default:
		return fmt.Sprintf("unknown tool %q", tu.Name), true
	}
}

func (r *run) runSQL(ctx context.Context, sql string) (string, bool) {
	maxRows := r.a.MaxRows
	if maxRows <= 0 {
		maxRows = DefaultMaxRows
	}
	start := time.Now()
	res, err := r.a.DB.Query(ctx, sql, maxRows)
	step := SQLStep{Turn: r.res.Turns, SQL: sql, DurationMS: time.Since(start).Milliseconds()}
	if err != nil {
		step.Error = err.Error()
		r.res.Steps = append(r.res.Steps, step)
		r.res.SQLErrors++
		return err.Error(), true
	}
	step.Rows = len(res.Rows)
	step.Capped = res.Truncated
	step.Output = FormatResult(res)
	r.res.Steps = append(r.res.Steps, step)
	return step.Output, false
}

func FormatResult(res db.Result) string {
	return fmt.Sprintf("rows: %d\ncapped: %t\n\n%s", len(res.Rows), res.Truncated, res.CSV())
}
