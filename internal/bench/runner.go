package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
)

const (
	DefaultRepeat      = 1
	DefaultConcurrency = 3

	ResultsDirName = "results"
	LatestFileName = "latest.json"
)

type Line struct {
	ID            string           `json:"id"`
	Difficulty    string           `json:"difficulty"`
	Repeat        int              `json:"repeat"`
	Config        string           `json:"config"`
	Provider      string           `json:"provider,omitempty"`
	Model         string           `json:"model,omitempty"`
	NoCache       bool             `json:"no_cache,omitempty"`
	NoEvidence    bool             `json:"no_evidence,omitempty"`
	Tier          llm.Tier         `json:"tier"`
	Correct       bool             `json:"correct"`
	CorrectStrict bool             `json:"correct_strict"`
	EXCorrect     *bool            `json:"ex_correct,omitempty"`
	FailReason    string           `json:"fail_reason,omitempty"`
	FormatReason  string           `json:"format_reason,omitempty"`
	Answer        string           `json:"answer"`
	Expected      any              `json:"expected"`
	SQL           string           `json:"sql"`
	Confidence    string           `json:"confidence"`
	Submitted     bool             `json:"submitted"`
	Failure       string           `json:"failure,omitempty"`
	Turns         int              `json:"turns"`
	SQLErrors     int              `json:"sql_errors"`
	Tokens        llm.Usage        `json:"tokens"`
	CostUSD       float64          `json:"cost_usd"`
	LatencyMS     int64            `json:"latency_ms"`
	CacheHitRatio float64          `json:"cache_hit_ratio"`
	JudgeVerdict  string           `json:"judge_verdict,omitempty"`
	JudgeCostUSD  float64          `json:"judge_cost_usd,omitempty"`
	RouteLabel    string           `json:"route_label,omitempty"`
	RouteReason   string           `json:"route_reason,omitempty"`
	RouteCostUSD  float64          `json:"route_cost_usd,omitempty"`
	RouteLatency  int64            `json:"route_latency_ms,omitempty"`
	RouteTokens   llm.Usage        `json:"route_tokens"`
	RouteRetry    string           `json:"route_retry,omitempty"`
	RouteGate     *router.GateInfo `json:"route_gate,omitempty"`
	ShadowCostUSD float64          `json:"shadow_cost_usd,omitempty"`
	Attempts      []Attempt        `json:"attempts,omitempty"`
	Error         string           `json:"error,omitempty"`
}

type Attempt struct {
	Tier          llm.Tier         `json:"tier"`
	Model         string           `json:"model,omitempty"`
	Signals       []string         `json:"signals,omitempty"`
	Verdict       string           `json:"verdict,omitempty"`
	VerdictReason string           `json:"verdict_reason,omitempty"`
	Correct       bool             `json:"correct"`
	Answer        string           `json:"answer"`
	Confidence    string           `json:"confidence"`
	Submitted     bool             `json:"submitted"`
	Turns         int              `json:"turns"`
	SQLErrors     int              `json:"sql_errors"`
	Tokens        llm.Usage        `json:"tokens"`
	CostUSD       float64          `json:"cost_usd"`
	VerifyCostUSD float64          `json:"verify_cost_usd,omitempty"`
	VerifyTokens  llm.Usage        `json:"verify_tokens"`
	VerifyGate    *router.GateInfo `json:"verify_gate,omitempty"`
	LatencyMS     int64            `json:"latency_ms"`
	Error         string           `json:"error,omitempty"`
}

type DifficultySummary struct {
	Total          int     `json:"total"`
	Correct        int     `json:"correct"`
	Accuracy       float64 `json:"accuracy"`
	CorrectStrict  int     `json:"correct_strict"`
	AccuracyStrict float64 `json:"accuracy_strict"`
	EXCorrect      int     `json:"ex_correct,omitempty"`
	EXAccuracy     float64 `json:"ex_accuracy,omitempty"`
}

type Summary struct {
	Config              string                       `json:"config"`
	Provider            string                       `json:"provider,omitempty"`
	NoCache             bool                         `json:"no_cache,omitempty"`
	NoEvidence          bool                         `json:"no_evidence,omitempty"`
	ContextLevels       string                       `json:"context_levels,omitempty"`
	Tag                 string                       `json:"tag,omitempty"`
	Timestamp           string                       `json:"timestamp"`
	Source              string                       `json:"source"`
	Repeat              int                          `json:"repeat"`
	Total               int                          `json:"total"`
	Correct             int                          `json:"correct"`
	Accuracy            float64                      `json:"accuracy"`
	CorrectStrict       int                          `json:"correct_strict"`
	AccuracyStrict      float64                      `json:"accuracy_strict"`
	ByDifficulty        map[string]DifficultySummary `json:"by_difficulty"`
	TotalCostUSD        float64                      `json:"total_cost_usd"`
	JudgeCostUSD        float64                      `json:"judge_cost_usd"`
	RouteCostUSD        float64                      `json:"route_cost_usd"`
	ShadowCostUSD       float64                      `json:"shadow_cost_usd,omitempty"`
	Confusion           map[string]map[llm.Tier]int  `json:"confusion"`
	RouteMatch          int                          `json:"route_match"`
	Escalation          map[string]EscalationSummary `json:"escalation,omitempty"`
	Signals             map[string]int               `json:"signals,omitempty"`
	Verifier            *VerifierSummary             `json:"verifier,omitempty"`
	FailReasons         map[string]int               `json:"fail_reasons,omitempty"`
	FormatReasons       map[string]int               `json:"format_reasons,omitempty"`
	CostPerCorrectUSD   float64                      `json:"cost_per_correct_usd"`
	EXCorrect           int                          `json:"ex_correct,omitempty"`
	EXTotal             int                          `json:"ex_total,omitempty"`
	EXAccuracy          float64                      `json:"ex_accuracy,omitempty"`
	CostPerEXCorrectUSD float64                      `json:"cost_per_ex_correct_usd,omitempty"`
	MeanLatencyMS       float64                      `json:"mean_latency_ms"`
	P95LatencyMS        int64                        `json:"p95_latency_ms"`
	Omni                *OmniMetrics                 `json:"omni,omitempty"`
	Regraded            *RegradeInfo                 `json:"regraded,omitempty"`
}

func ExpectedTier(difficulty string) llm.Tier {
	switch difficulty {
	case DifficultyEasy, DifficultySimple:
		return llm.TierCheap
	case DifficultyModerate:
		return llm.TierMid
	default:
		return llm.TierTop
	}
}

func CacheHitRatio(u llm.Usage) float64 {
	total := u.PromptTokens()
	if total == 0 {
		return 0
	}
	return float64(u.CacheReadInputTokens) / float64(total)
}

func Summarize(config, timestamp, source string, repeat int, lines []Line) Summary {
	s := Summary{
		Config:       config,
		Timestamp:    timestamp,
		Source:       source,
		Repeat:       repeat,
		Total:        len(lines),
		ByDifficulty: map[string]DifficultySummary{},
		Confusion:    map[string]map[llm.Tier]int{},
	}
	var latencies []int64
	for _, l := range lines {
		if l.Correct {
			s.Correct++
		}
		if l.CorrectStrict {
			s.CorrectStrict++
		}
		if l.EXCorrect != nil {
			s.EXTotal++
			if *l.EXCorrect {
				s.EXCorrect++
			}
		}
		s.TotalCostUSD += l.CostUSD
		s.JudgeCostUSD += l.JudgeCostUSD
		s.RouteCostUSD += l.RouteCostUSD
		s.ShadowCostUSD += l.ShadowCostUSD
		if l.Tier != "" {
			if s.Confusion[l.Difficulty] == nil {
				s.Confusion[l.Difficulty] = map[llm.Tier]int{}
			}
			s.Confusion[l.Difficulty][l.Tier]++
			if l.Tier == ExpectedTier(l.Difficulty) {
				s.RouteMatch++
			}
		}
		latencies = append(latencies, l.LatencyMS)
		d := s.ByDifficulty[l.Difficulty]
		d.Total++
		if l.Correct {
			d.Correct++
		}
		if l.CorrectStrict {
			d.CorrectStrict++
		}
		if l.EXCorrect != nil && *l.EXCorrect {
			d.EXCorrect++
		}
		s.ByDifficulty[l.Difficulty] = d
	}
	if s.Total > 0 {
		s.Accuracy = float64(s.Correct) / float64(s.Total)
		s.AccuracyStrict = float64(s.CorrectStrict) / float64(s.Total)
		s.EXAccuracy = float64(s.EXCorrect) / float64(s.Total)
	}
	for k, d := range s.ByDifficulty {
		if d.Total > 0 {
			d.Accuracy = float64(d.Correct) / float64(d.Total)
			d.AccuracyStrict = float64(d.CorrectStrict) / float64(d.Total)
			d.EXAccuracy = float64(d.EXCorrect) / float64(d.Total)
		}
		s.ByDifficulty[k] = d
	}
	summarizeCascade(&s, lines)
	s.FailReasons, s.FormatReasons = CountReasons(lines)
	if s.Correct > 0 {
		s.CostPerCorrectUSD = (s.TotalCostUSD + s.JudgeCostUSD) / float64(s.Correct)
	}
	if s.EXCorrect > 0 {
		s.CostPerEXCorrectUSD = (s.TotalCostUSD + s.JudgeCostUSD) / float64(s.EXCorrect)
	}
	if len(latencies) > 0 {
		var sum int64
		for _, l := range latencies {
			sum += l
		}
		s.MeanLatencyMS = float64(sum) / float64(len(latencies))
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		idx := (len(latencies)*95+99)/100 - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(latencies) {
			idx = len(latencies) - 1
		}
		s.P95LatencyMS = latencies[idx]
	}
	return s
}

func ConfigDir(provider, config string) string {
	if provider == "" || provider == llm.ProviderAnthropic {
		return filepath.Join(ResultsDirName, config)
	}
	return filepath.Join(ResultsDirName, provider, config)
}

func LatestPath(provider, config string) string {
	return filepath.Join(ConfigDir(provider, config), LatestFileName)
}

func BirdConfigDir(provider, config string) string {
	if provider == "" || provider == llm.ProviderAnthropic {
		return filepath.Join(ResultsDirName, BirdDirName, config)
	}
	return filepath.Join(ResultsDirName, BirdDirName, provider, config)
}

func WriteLines(path string, lines []Line) error {
	return WriteJSONL(path, lines)
}

func WriteJSONL[T any](path string, rows []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func ReadJSONL[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []T
	for _, line := range splitJSONLines(string(data)) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r T
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func WriteSummary(path string, s Summary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func ReadLines(path string) ([]Line, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Line
	for _, line := range splitJSONLines(string(data)) {
		l, err := DecodeLine(line)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

func DecodeLine(raw string) (Line, error) {
	var l Line
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		return Line{}, err
	}
	if !strings.Contains(raw, `"correct_strict":`) {
		l.CorrectStrict = l.Correct
	}
	return l, nil
}

func splitJSONLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if line := s[start:i]; len(line) > 0 {
				out = append(out, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

type AgentRunner interface {
	Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error)
}

type Solver interface {
	Name() string
	Solve(ctx context.Context, question string) (router.Outcome, error)
}

type Runner struct {
	Agent       AgentRunner
	Judge       JudgeCaller
	Router      router.Router
	Cascade     Solver
	AgentFor    func(Question) AgentRunner
	RouterFor   func(Question) router.Router
	CascadeFor  func(Question) Solver
	EX          EXGrader
	Questions   []Question
	Expected    map[string]any
	Config      string
	Provider    string
	NoCache     bool
	NoEvidence  bool
	Repeat      int
	Concurrency int
	Traces      *TraceCollector
}

type job struct {
	index    int
	question Question
	repeat   int
}

func (r *Runner) jobs() []job {
	var out []job
	repeat := r.Repeat
	if repeat < 1 {
		repeat = 1
	}
	for rep := 1; rep <= repeat; rep++ {
		for _, q := range r.Questions {
			out = append(out, job{index: len(out), question: q, repeat: rep})
		}
	}
	return out
}

func (r *Runner) Run(ctx context.Context) ([]Line, error) {
	all := r.jobs()
	if len(all) == 0 {
		return nil, fmt.Errorf("no questions to run")
	}
	concurrency := r.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	lines := make([]Line, len(all))
	lines[0] = r.runExplained(ctx, all[0])
	if ctx.Err() != nil {
		return lines[:1], ctx.Err()
	}
	if len(all) == 1 {
		return lines, nil
	}
	sem := make(chan struct{}, concurrency)
	done := make(chan struct{}, len(all)-1)
	for _, j := range all[1:] {
		j := j
		sem <- struct{}{}
		go func() {
			lines[j.index] = r.runExplained(ctx, j)
			<-sem
			done <- struct{}{}
		}()
	}
	for range all[1:] {
		<-done
	}
	return lines, nil
}

func (r *Runner) runExplained(ctx context.Context, j job) Line {
	l := r.runOne(ctx, j)
	ApplyReasons(j.question, &l)
	return l
}

func (r *Runner) runOne(ctx context.Context, j job) Line {
	var trecs []TraceRecord
	defer func() {
		r.Traces.AddAll(j.index, trecs)
	}()
	l := Line{
		ID:         j.question.ID,
		Difficulty: j.question.Difficulty,
		Repeat:     j.repeat,
		Config:     r.Config,
		Provider:   r.Provider,
		NoCache:    r.NoCache,
		NoEvidence: r.NoEvidence,
		Expected:   r.Expected[j.question.ID],
	}
	if r.CascadeFor != nil {
		return r.runCascade(ctx, j, l)
	}
	if r.Cascade != nil {
		return r.runCascade(ctx, j, l)
	}
	rt := r.Router
	if r.RouterFor != nil {
		rt = r.RouterFor(j.question)
	}
	if rt == nil {
		l.Error = "runner has no router"
		return l
	}
	d, err := rt.Route(ctx, j.question.Text)
	if err != nil {
		l.RouteRetry = err.Error()
		retry, rerr := rt.Route(ctx, j.question.Text)
		retry.Cost += d.Cost
		retry.ShadowCostUSD += d.ShadowCostUSD
		retry.Latency += d.Latency
		retry.CallRecords = append(d.CallRecords, retry.CallRecords...)
		d, err = retry, rerr
	}
	for _, rec := range d.CallRecords {
		l.RouteTokens = l.RouteTokens.Add(rec.Usage)
	}
	trecs = appendTraceRecords(trecs, j.question.ID, j.repeat, d.Tier, d.CallRecords)
	l.RouteCostUSD = d.Cost
	l.RouteLatency = d.Latency.Milliseconds()
	l.RouteGate = d.Gate
	l.ShadowCostUSD = d.ShadowCostUSD
	l.CostUSD = d.Cost
	l.LatencyMS = l.RouteLatency
	if err != nil {
		l.Error = "route: " + err.Error()
		return l
	}
	l.Tier = d.Tier
	l.RouteLabel = d.Label
	l.RouteReason = d.Reason
	ag := r.Agent
	if r.AgentFor != nil {
		ag = r.AgentFor(j.question)
	}
	if ag == nil {
		l.Error = "runner has no agent"
		return l
	}
	res, err := ag.Run(ctx, j.question.Text, d.Tier)
	if err != nil {
		trecs = appendTraceRecords(trecs, j.question.ID, j.repeat, d.Tier, res.Records)
		first := res
		res, err = ag.Run(ctx, j.question.Text, d.Tier)
		res.Usage = first.Usage.Add(res.Usage)
		res.CostUSD += first.CostUSD
		res.WallMS += first.WallMS
		if err != nil {
			trecs = appendTraceRecords(trecs, j.question.ID, j.repeat, d.Tier, res.Records)
			l.Tokens = res.Usage
			l.CostUSD += res.CostUSD
			l.LatencyMS += res.WallMS
			l.Error = err.Error()
			return l
		}
	}
	trecs = appendTraceRecords(trecs, j.question.ID, j.repeat, d.Tier, res.Records)
	if n := len(res.Records); n > 0 {
		l.Model = res.Records[n-1].Model
	}
	l.Answer = res.Answer
	l.SQL = res.SQL
	l.Confidence = res.Confidence
	l.Submitted = res.Submitted
	l.Failure = res.Failure
	l.Turns = res.Turns
	l.SQLErrors = res.SQLErrors
	l.Tokens = res.Usage
	l.CostUSD += res.CostUSD
	l.LatencyMS += res.WallMS
	l.CacheHitRatio = CacheHitRatio(res.Usage)
	if rec := r.grade(ctx, j.question, res.Answer, &l); rec != nil {
		trecs = append(trecs, NewTraceRecord(j.question.ID, j.repeat, d.Tier, *rec))
	}
	r.gradeEX(ctx, j.question, res.SQL, &l)
	return l
}

func (r *Runner) gradeEX(ctx context.Context, q Question, modelSQL string, l *Line) {
	if r.EX == nil {
		return
	}
	ok, err := r.EX.ExecCorrect(ctx, q, modelSQL)
	if err != nil {
		if l.Error == "" {
			l.Error = err.Error()
		}
		return
	}
	l.EXCorrect = &ok
}

func (r *Runner) grade(ctx context.Context, q Question, answer string, l *Line) *llm.CallRecord {
	if q.AnswerType == TypeFreeText {
		if r.Judge == nil {
			l.Error = "free_text question needs a judge"
			return nil
		}
		jr, err := JudgeFreeText(ctx, r.Judge, q, l.Expected, answer)
		if err != nil {
			spent := jr.CostUSD
			jr, err = JudgeFreeText(ctx, r.Judge, q, l.Expected, answer)
			jr.CostUSD += spent
			if err != nil {
				l.JudgeCostUSD += jr.CostUSD
				l.Error = err.Error()
				return nil
			}
		}
		l.Correct = jr.Correct
		l.CorrectStrict = jr.Correct
		l.JudgeVerdict = jr.Verdict
		l.JudgeCostUSD += jr.CostUSD
		rec := jr.Record
		return &rec
	}
	correct, err := Grade(q, l.Expected, answer)
	if err != nil {
		l.Error = err.Error()
		return nil
	}
	strict, err := GradeStrict(q, l.Expected, answer)
	if err != nil {
		l.Error = err.Error()
		return nil
	}
	l.Correct = correct
	l.CorrectStrict = strict
	return nil
}

func (r *Runner) runCascade(ctx context.Context, j job, l Line) Line {
	var trecs []TraceRecord
	defer func() {
		r.Traces.AddAll(j.index, trecs)
	}()
	cas := r.Cascade
	if r.CascadeFor != nil {
		cas = r.CascadeFor(j.question)
	}
	if cas == nil {
		l.Error = "runner has no cascade"
		return l
	}
	o, err := cas.Solve(ctx, j.question.Text)
	l.CostUSD = o.AgentCost() + o.OverheadCost()
	l.RouteCostUSD = o.OverheadCost()
	l.ShadowCostUSD = o.ShadowCost()
	l.RouteTokens = o.OverheadUsage()
	l.RouteLatency = verifyLatency(o).Milliseconds()
	l.LatencyMS = o.Latency().Milliseconds()
	l.Tokens = o.AgentUsage()
	l.RouteLabel = o.Path()
	l.RouteReason = o.Reason()
	for i, a := range o.Attempts {
		at := Attempt{
			Tier:          a.Tier,
			Signals:       a.Signals,
			Answer:        a.Result.Answer,
			Confidence:    a.Result.Confidence,
			Submitted:     a.Result.Submitted,
			Turns:         a.Result.Turns,
			SQLErrors:     a.Result.SQLErrors,
			Tokens:        a.Result.Usage,
			CostUSD:       a.Result.CostUSD,
			VerifyCostUSD: a.VerifyCost,
			VerifyGate:    a.VerifyGate,
			LatencyMS:     a.Result.WallMS + a.VerifyLatency.Milliseconds(),
			Error:         a.Err,
		}
		for _, rec := range a.VerifyRecords {
			at.VerifyTokens = at.VerifyTokens.Add(rec.Usage)
		}
		if n := len(a.Result.Records); n > 0 {
			at.Model = a.Result.Records[n-1].Model
		}
		if a.Verified {
			at.Verdict = a.Verdict.Verdict
			at.VerdictReason = a.Verdict.Reason
		}
		trecs = appendTraceRecords(trecs, j.question.ID, j.repeat, a.Tier, a.Result.Records)
		trecs = appendTraceRecords(trecs, j.question.ID, j.repeat, a.Tier, a.VerifyRecords)
		if a.Err == "" && i < len(o.Attempts)-1 {
			scratch := Line{Expected: l.Expected}
			if rec := r.grade(ctx, j.question, a.Result.Answer, &scratch); rec != nil {
				trecs = append(trecs, NewTraceRecord(j.question.ID, j.repeat, a.Tier, *rec))
			}
			at.Correct = scratch.Correct
			l.JudgeCostUSD += scratch.JudgeCostUSD
		}
		l.Attempts = append(l.Attempts, at)
	}
	if err != nil {
		l.Error = err.Error()
		return l
	}
	if len(l.Attempts) == 0 {
		l.Error = "cascade returned no attempts"
		return l
	}
	fi := o.FinalIndex()
	if fi < 0 || fi >= len(l.Attempts) {
		fi = len(l.Attempts) - 1
	}
	final := o.Final()
	res := final.Result
	l.Tier = final.Tier
	l.Model = l.Attempts[fi].Model
	l.Answer = res.Answer
	l.SQL = res.SQL
	l.Confidence = res.Confidence
	l.Submitted = res.Submitted
	l.Failure = res.Failure
	l.Turns = res.Turns
	l.SQLErrors = res.SQLErrors
	l.CacheHitRatio = CacheHitRatio(l.Tokens)
	if rec := r.grade(ctx, j.question, res.Answer, &l); rec != nil {
		trecs = append(trecs, NewTraceRecord(j.question.ID, j.repeat, final.Tier, *rec))
	}
	r.gradeEX(ctx, j.question, res.SQL, &l)
	l.Attempts[fi].Correct = l.Correct
	return l
}

func verifyLatency(o router.Outcome) time.Duration {
	var total time.Duration
	for _, a := range o.Attempts {
		total += a.VerifyLatency
	}
	return total
}
