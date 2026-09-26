package router

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"

	"github.com/KranzL/omni-example/internal/llm"
)

type GateInfo struct {
	Primary        string             `json:"primary"`
	PrimaryLabel   string             `json:"primary_label,omitempty"`
	PrimaryScore   float64            `json:"primary_score"`
	PrimaryProbs   map[string]float64 `json:"primary_probs,omitempty"`
	PrimaryCost    float64            `json:"primary_cost_usd"`
	PrimaryLatency int64              `json:"primary_latency_ms"`
	PrimaryError   string             `json:"primary_error,omitempty"`
	Threshold      float64            `json:"threshold"`
	FellBack       bool               `json:"fell_back"`
	FallbackReason string             `json:"fallback_reason,omitempty"`
	Fallback       string             `json:"fallback,omitempty"`
	FallbackLabel  string             `json:"fallback_label,omitempty"`
	FallbackCost   float64            `json:"fallback_cost_usd,omitempty"`
	FallbackMS     int64              `json:"fallback_latency_ms,omitempty"`
	Shadowed       bool               `json:"shadowed,omitempty"`
	ShadowLabel    string             `json:"shadow_label,omitempty"`
	ShadowCost     float64            `json:"shadow_cost_usd,omitempty"`
}

const (
	FallbackLowScore = "low_score"
	FallbackError    = "primary_error"
	FallbackUnscored = "unscored"
	FallbackLabel    = "unknown_label"
)

type Gated struct {
	Label            string
	Primary          Decider
	Fallback         Decider
	Thresholds       map[string]float64
	DefaultThreshold float64
	ShadowRate       float64
	Sample           func() float64
	OnShadow         func(in DecisionInput, primary, shadow Choice)
}

func (g *Gated) Name() string {
	if g.Label != "" {
		return g.Label
	}
	return g.Primary.Name() + ">" + g.Fallback.Name()
}

func (g *Gated) Point() string {
	return g.Fallback.Point()
}

func (g *Gated) Labels() []string {
	return g.Fallback.Labels()
}

func (g *Gated) Threshold(label string) float64 {
	if t, ok := g.Thresholds[label]; ok {
		return t
	}
	return g.DefaultThreshold
}

func (g *Gated) Decide(ctx context.Context, in DecisionInput) (Choice, error) {
	p, perr := g.Primary.Decide(ctx, in)
	info := &GateInfo{
		Primary:        g.Primary.Name(),
		PrimaryLabel:   p.Label,
		PrimaryScore:   p.Score,
		PrimaryProbs:   p.Probs,
		PrimaryCost:    p.Cost,
		PrimaryLatency: p.Latency.Milliseconds(),
		Threshold:      g.Threshold(p.Label),
	}
	switch {
	case perr != nil:
		info.PrimaryError = perr.Error()
		info.FallbackReason = FallbackError
	case !p.Scored:
		info.FallbackReason = FallbackUnscored
	case !slices.Contains(g.Fallback.Labels(), p.Label):
		info.FallbackReason = FallbackLabel
	case p.Score < info.Threshold:
		info.FallbackReason = FallbackLowScore
	}
	if info.FallbackReason == "" {
		p.Gate = info
		p.Reason = fmt.Sprintf("%s %s score %.2f >= %.2f; %s", info.Primary, p.Label, p.Score, info.Threshold, p.Reason)
		if g.ShadowRate > 0 && g.sample() < g.ShadowRate {
			s, serr := g.Fallback.Decide(ctx, in)
			info.Shadowed = true
			info.ShadowCost = s.Cost
			p.ShadowCostUSD = s.Cost
			if serr == nil {
				info.ShadowLabel = s.Label
				if g.OnShadow != nil {
					g.OnShadow(in, p, s)
				}
			}
		}
		return p, nil
	}
	f, ferr := g.Fallback.Decide(ctx, in)
	info.FellBack = true
	info.Fallback = g.Fallback.Name()
	info.FallbackLabel = f.Label
	info.FallbackCost = f.Cost
	info.FallbackMS = f.Latency.Milliseconds()
	out := f
	out.Gate = info
	out.Cost = p.Cost + f.Cost
	out.Latency = p.Latency + f.Latency
	out.CallRecords = append(append([]llm.CallRecord{}, p.CallRecords...), f.CallRecords...)
	out.Reason = fmt.Sprintf("%s %s score %.2f < %.2f (%s), %s: %s", info.Primary, labelOrNone(p.Label), p.Score, info.Threshold, info.FallbackReason, info.Fallback, f.Reason)
	return out, ferr
}

func (g *Gated) sample() float64 {
	if g.Sample != nil {
		return g.Sample()
	}
	return rand.Float64()
}

func labelOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

type DecisionRouter struct {
	Label   string
	Decider Decider
}

func (r *DecisionRouter) Name() string {
	return r.Label
}

func (r *DecisionRouter) Route(ctx context.Context, question string) (Decision, error) {
	ch, err := r.Decider.Decide(ctx, DecisionInput{Question: question})
	d := Decision{Cost: ch.Cost, ShadowCostUSD: ch.ShadowCostUSD, Latency: ch.Latency, CallRecords: ch.CallRecords, Gate: ch.Gate}
	if err != nil {
		return d, err
	}
	tier, err := TierForLabel(ch.Label)
	if err != nil {
		return d, err
	}
	d.Tier = tier
	d.Label = ch.Label
	d.Reason = ch.Reason
	return d, nil
}
