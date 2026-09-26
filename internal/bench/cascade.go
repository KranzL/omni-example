package bench

import (
	"fmt"
	"strings"

	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
)

type EscalationSummary struct {
	Total      int     `json:"total"`
	Escalated  int     `json:"escalated"`
	Rate       float64 `json:"rate"`
	ReachedTop int     `json:"reached_top"`
}

type VerifierSummary struct {
	Verified        int     `json:"verified"`
	Accepted        int     `json:"accepted"`
	Rejected        int     `json:"rejected"`
	Errors          int     `json:"errors"`
	AcceptedCorrect int     `json:"accepted_correct"`
	AcceptedWrong   int     `json:"accepted_wrong"`
	RejectedCorrect int     `json:"rejected_correct"`
	RejectedWrong   int     `json:"rejected_wrong"`
	FalseRejectRate float64 `json:"false_reject_rate"`
	FalseAcceptRate float64 `json:"false_accept_rate"`
}

func summarizeCascade(s *Summary, lines []Line) {
	cascade := false
	for _, l := range lines {
		if len(l.Attempts) > 0 {
			cascade = true
			break
		}
	}
	if !cascade {
		return
	}
	s.Escalation = map[string]EscalationSummary{}
	s.Signals = map[string]int{}
	var v VerifierSummary
	for _, l := range lines {
		e := s.Escalation[l.Difficulty]
		e.Total++
		if len(l.Attempts) > 1 {
			e.Escalated++
		}
		if n := len(l.Attempts); n > 0 && l.Attempts[n-1].Tier == llm.TierTop && n > 1 {
			e.ReachedTop++
		}
		s.Escalation[l.Difficulty] = e
		for _, a := range l.Attempts {
			for _, sig := range a.Signals {
				s.Signals[sig]++
			}
			switch a.Verdict {
			case router.VerdictAccept:
				v.Verified++
				v.Accepted++
				if a.Correct {
					v.AcceptedCorrect++
				} else {
					v.AcceptedWrong++
				}
			case router.VerdictReject:
				v.Verified++
				v.Rejected++
				if a.Correct {
					v.RejectedCorrect++
				} else {
					v.RejectedWrong++
				}
			case router.VerdictError:
				v.Verified++
				v.Errors++
			}
		}
	}
	for k, e := range s.Escalation {
		if e.Total > 0 {
			e.Rate = float64(e.Escalated) / float64(e.Total)
		}
		s.Escalation[k] = e
	}
	if n := v.AcceptedCorrect + v.RejectedCorrect; n > 0 {
		v.FalseRejectRate = float64(v.RejectedCorrect) / float64(n)
	}
	if n := v.AcceptedWrong + v.RejectedWrong; n > 0 {
		v.FalseAcceptRate = float64(v.AcceptedWrong) / float64(n)
	}
	if v.Verified > 0 {
		s.Verifier = &v
	}
}

func CascadeReport(s Summary) string {
	if s.Escalation == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("| difficulty | questions | escalated | rate | reached top |\n|---|---:|---:|---:|---:|\n")
	for _, d := range confusionRows {
		e, ok := s.Escalation[d]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %.2f | %d |\n", d, e.Total, e.Escalated, e.Rate, e.ReachedTop)
	}
	var sigs []string
	for _, sig := range []string{router.SignalLowConfidence, router.SignalNoSubmit, router.SignalSQLErrors, router.SignalZeroRows, router.SignalVerifierReject, router.SignalVerifierError, router.SignalAgentError} {
		if n := s.Signals[sig]; n > 0 {
			sigs = append(sigs, fmt.Sprintf("%s=%d", sig, n))
		}
	}
	fmt.Fprintf(&b, "signals fired: %s\n", strings.Join(sigs, " "))
	if v := s.Verifier; v != nil {
		fmt.Fprintf(&b, "verifier: verified=%d accepted=%d (correct %d, wrong %d) rejected=%d (correct %d, wrong %d) errors=%d false_reject_rate=%.3f false_accept_rate=%.3f\n",
			v.Verified, v.Accepted, v.AcceptedCorrect, v.AcceptedWrong, v.Rejected, v.RejectedCorrect, v.RejectedWrong, v.Errors, v.FalseRejectRate, v.FalseAcceptRate)
	}
	return b.String()
}
