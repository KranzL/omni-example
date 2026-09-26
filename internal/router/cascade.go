package router

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	NameCascadeSignals      = "cascade-signals"
	NameCascadeVerifyHaiku  = "cascade-verify-haiku"
	NameCascadeVerifySonnet = "cascade-verify-sonnet"

	SignalLowConfidence  = "low_confidence"
	SignalNoSubmit       = "no_submit"
	SignalSQLErrors      = "sql_errors"
	SignalZeroRows       = "zero_rows"
	SignalVerifierReject = "verifier_reject"
	SignalVerifierError  = "verifier_error"
	SignalAgentError     = "agent_error"

	DefaultMaxSQLErrors = 2
)

func CascadeNames() []string {
	return []string{NameCascadeSignals, NameCascadeVerifyHaiku, NameCascadeVerifySonnet, NameCascadeVerifyJev}
}

func IsCascade(name string) bool {
	for _, n := range CascadeNames() {
		if n == name {
			return true
		}
	}
	return false
}

func CascadeVerifierTier(name string) (llm.Tier, bool) {
	switch name {
	case NameCascadeVerifyHaiku, NameCascadeVerifyJev:
		return llm.TierCheap, true
	case NameCascadeVerifySonnet:
		return llm.TierMid, true
	default:
		return "", false
	}
}

type Signals struct {
	LowConfidence bool `json:"low_confidence"`
	NoSubmit      bool `json:"no_submit"`
	SQLErrors     bool `json:"sql_errors"`
	ZeroRows      bool `json:"zero_rows"`
	Verifier      bool `json:"verifier"`
	MaxSQLErrors  int  `json:"max_sql_errors"`
}

func DefaultSignals() Signals {
	return Signals{LowConfidence: true, NoSubmit: true, SQLErrors: true, ZeroRows: true, MaxSQLErrors: DefaultMaxSQLErrors}
}

func (s Signals) Fired(res agent.Result) []string {
	var out []string
	if s.LowConfidence && LowConfidence(res.Confidence) {
		out = append(out, SignalLowConfidence)
	}
	if s.NoSubmit && (!res.Submitted || res.Nudged) {
		out = append(out, SignalNoSubmit)
	}
	maxErrs := s.MaxSQLErrors
	if maxErrs <= 0 {
		maxErrs = DefaultMaxSQLErrors
	}
	if s.SQLErrors && res.SQLErrors > maxErrs {
		out = append(out, SignalSQLErrors)
	}
	if s.ZeroRows {
		if rows, ok := FinalRows(res); ok && rows == 0 {
			out = append(out, SignalZeroRows)
		}
	}
	return out
}

func LowConfidence(c string) bool {
	switch strings.ToLower(strings.TrimSpace(c)) {
	case "high", "medium":
		return false
	default:
		return true
	}
}

func FinalRows(res agent.Result) (int, bool) {
	if !res.Submitted {
		return 0, false
	}
	if step, ok := SubmittedStep(res); ok {
		if step.Error != "" {
			return 0, true
		}
		return step.Rows, true
	}
	for i := len(res.Steps) - 1; i >= 0; i-- {
		if res.Steps[i].Error == "" {
			return res.Steps[i].Rows, true
		}
	}
	return 0, false
}

type AgentRunner interface {
	Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error)
}

type Attempt struct {
	Tier          llm.Tier
	Result        agent.Result
	Err           string
	Signals       []string
	Verified      bool
	Verdict       Verdict
	VerifyCost    float64
	VerifyShadow  float64
	VerifyLatency time.Duration
	VerifyRecords []llm.CallRecord
	VerifyGate    *GateInfo
}

func (a Attempt) Escalate() bool {
	return len(a.Signals) > 0
}

type Outcome struct {
	Attempts []Attempt
}

func (o Outcome) Final() Attempt {
	if len(o.Attempts) == 0 {
		return Attempt{}
	}
	return o.Attempts[o.FinalIndex()]
}

func (o Outcome) FinalIndex() int {
	for i := len(o.Attempts) - 1; i >= 0; i-- {
		if o.Attempts[i].Err == "" {
			return i
		}
	}
	return len(o.Attempts) - 1
}

func (o Outcome) AgentCost() float64 {
	var total float64
	for _, a := range o.Attempts {
		total += a.Result.CostUSD
	}
	return total
}

func (o Outcome) OverheadCost() float64 {
	var total float64
	for _, a := range o.Attempts {
		total += a.VerifyCost
	}
	return total
}

func (o Outcome) ShadowCost() float64 {
	var total float64
	for _, a := range o.Attempts {
		total += a.VerifyShadow
	}
	return total
}

func (o Outcome) AgentUsage() llm.Usage {
	var total llm.Usage
	for _, a := range o.Attempts {
		total = total.Add(a.Result.Usage)
	}
	return total
}

func (o Outcome) OverheadUsage() llm.Usage {
	var total llm.Usage
	for _, a := range o.Attempts {
		for _, r := range a.VerifyRecords {
			total = total.Add(r.Usage)
		}
	}
	return total
}

func (o Outcome) Latency() time.Duration {
	var total time.Duration
	for _, a := range o.Attempts {
		total += time.Duration(a.Result.WallMS)*time.Millisecond + a.VerifyLatency
	}
	return total
}

func (o Outcome) Path() string {
	var tiers []string
	for _, a := range o.Attempts {
		tiers = append(tiers, string(a.Tier))
	}
	return strings.Join(tiers, ">")
}

func (o Outcome) Reason() string {
	var parts []string
	for _, a := range o.Attempts {
		switch {
		case a.Escalate():
			parts = append(parts, fmt.Sprintf("%s: %s", a.Tier, strings.Join(a.Signals, ",")))
		case a.Verified:
			parts = append(parts, fmt.Sprintf("%s: %s", a.Tier, a.Verdict.Verdict))
		default:
			parts = append(parts, fmt.Sprintf("%s: final", a.Tier))
		}
	}
	return strings.Join(parts, "; ")
}

type Cascade struct {
	Label    string
	Agent    AgentRunner
	Tiers    []llm.Tier
	Signals  Signals
	Verifier *Verifier
}

func NewCascade(name string, a AgentRunner, v *Verifier) *Cascade {
	s := DefaultSignals()
	s.Verifier = v != nil
	return &Cascade{Label: name, Agent: a, Tiers: llm.Tiers, Signals: s, Verifier: v}
}

func (c *Cascade) Name() string {
	return c.Label
}

func (c *Cascade) Solve(ctx context.Context, question string) (Outcome, error) {
	var out Outcome
	tiers := c.Tiers
	if len(tiers) == 0 {
		tiers = llm.Tiers
	}
	for i, tier := range tiers {
		last := i == len(tiers)-1
		att := Attempt{Tier: tier}
		res, err := c.Agent.Run(ctx, question, tier)
		if err != nil && ctx.Err() == nil {
			failed := res
			res, err = c.Agent.Run(ctx, question, tier)
			res = addSpend(res, failed)
		}
		att.Result = res
		if err != nil {
			att.Err = err.Error()
			if ctx.Err() != nil {
				out.Attempts = append(out.Attempts, att)
				return out, err
			}
			att.Signals = []string{SignalAgentError}
			out.Attempts = append(out.Attempts, att)
			if last {
				if out.Final().Err == "" {
					return out, nil
				}
				return out, err
			}
			continue
		}
		if last {
			out.Attempts = append(out.Attempts, att)
			return out, nil
		}
		att.Signals = c.Signals.Fired(res)
		if len(att.Signals) == 0 && c.Signals.Verifier && c.Verifier != nil {
			vr, verr := c.Verifier.Verify(ctx, question, res)
			att.Verified = true
			att.VerifyCost = vr.Cost
			att.VerifyShadow = vr.ShadowCostUSD
			att.VerifyLatency = vr.Latency
			att.VerifyRecords = vr.CallRecords
			att.VerifyGate = vr.Gate
			att.Verdict = vr.Verdict
			if verr != nil {
				att.Verdict = Verdict{Verdict: VerdictError, Reason: verr.Error()}
			}
			switch att.Verdict.Verdict {
			case VerdictReject:
				att.Signals = []string{SignalVerifierReject}
			case VerdictAccept:
			default:
				att.Signals = []string{SignalVerifierError}
			}
		}
		out.Attempts = append(out.Attempts, att)
		if !att.Escalate() {
			return out, nil
		}
	}
	return out, nil
}

func addSpend(res, failed agent.Result) agent.Result {
	res.CostUSD += failed.CostUSD
	res.Usage = failed.Usage.Add(res.Usage)
	res.WallMS += failed.WallMS
	res.CacheRead += failed.CacheRead
	res.CacheCreation += failed.CacheCreation
	if len(failed.Records) > 0 {
		res.Records = append(append([]llm.CallRecord{}, failed.Records...), res.Records...)
	}
	return res
}
