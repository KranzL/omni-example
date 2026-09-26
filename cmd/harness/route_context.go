package main

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/llm"
)

var (
	sequencePattern = regexp.MustCompile(`(?i)\b(first|second|third|nth|repeat)\b.*\borders?\b`)
	perUserPattern  = regexp.MustCompile(`(?i)\b(per-user|per user|each user's)\b`)
	statPattern     = regexp.MustCompile(`(?i)\b(median|percentile)\b`)
)

const sequenceHint = `Context for this question:
- One order_id owns several order_items rows with slightly different created_at values, so the time of an order is MIN(created_at) over its items. Rank and difference orders, never raw items.
Worked example:
Question: What was the median number of days between consecutive non-cancelled orders for users created in 2023?
SQL:
WITH orders AS (SELECT oi.user_id, oi.order_id, MIN(oi.created_at) AS ordered_at FROM public.order_items oi JOIN public.users u ON u.id = oi.user_id WHERE oi.status <> 'Cancelled' AND u.created_at >= '2023-01-01' AND u.created_at < '2024-01-01' GROUP BY oi.user_id, oi.order_id), gaps AS (SELECT user_id, ordered_at, LAG(ordered_at) OVER (PARTITION BY user_id ORDER BY ordered_at) AS prev_at FROM orders) SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM ordered_at - prev_at) / 86400) AS median_days FROM gaps WHERE prev_at IS NOT NULL`

const perUserHint = `Context for this question:
- When a question groups by user, order, or session and then aggregates across those groups with a median, a percentile, or a count, filter rows with WHERE on status, never with SUM(CASE WHEN ...) inside the grouped query, because the CASE form keeps zero-value groups that shift the result.
Worked example:
Question: What was the median per-user net spend in 2023?
SQL:
WITH per_user AS (SELECT oi.user_id, SUM(oi.sale_price) AS net_spend FROM public.order_items oi WHERE oi.status NOT IN ('Cancelled', 'Returned') AND oi.created_at >= '2023-01-01' AND oi.created_at < '2024-01-01' GROUP BY oi.user_id) SELECT PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY net_spend) AS median_net_spend FROM per_user`

func routeContextHints(question string) []string {
	var hints []string
	if sequencePattern.MatchString(question) {
		hints = append(hints, sequenceHint)
	}
	if perUserPattern.MatchString(question) && statPattern.MatchString(question) {
		hints = append(hints, perUserHint)
	}
	return hints
}

func withRouteContext(question string) string {
	hints := routeContextHints(question)
	if len(hints) == 0 {
		return question
	}
	return question + "\n\n" + strings.Join(hints, "\n\n")
}

type contextRoutingAgent struct {
	inner bench.AgentRunner
}

func (a contextRoutingAgent) Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error) {
	return a.inner.Run(ctx, withRouteContext(question), tier)
}

var errCheapThinkingProvider = errors.New("--cheap-thinking needs the anthropic provider")

type settingsProvider interface {
	Settings(t llm.Tier) llm.Settings
	SetSettings(t llm.Tier, s llm.Settings) error
}

func enableCheapThinking(client any, budget int64) error {
	sp, ok := client.(settingsProvider)
	if !ok {
		return errCheapThinkingProvider
	}
	s := sp.Settings(llm.TierCheap)
	s.Thinking = llm.ThinkingBudget
	s.ThinkingBudget = budget
	s.MaxTokens = budget + 4096
	return sp.SetSettings(llm.TierCheap, s)
}
