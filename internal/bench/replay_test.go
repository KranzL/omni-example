package bench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/router"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata golden files")

const (
	replayGoldenPath = "testdata/decider_replay.golden"
	replaySemantic   = "SEMANTIC LAYER FIXTURE"
)

var replayRuns = map[string]string{
	router.NameClassifier:         "20260924T043814Z.jsonl",
	router.NameCascadeVerifyHaiku: "20260924T050212Z.jsonl",
}

type replayProvider struct {
	mu       sync.Mutex
	routes   map[string]Line
	verdicts map[string][]Attempt
	used     map[string]int
	reqs     []llm.Request
}

func (p *replayProvider) Name() string { return "replay" }

func (p *replayProvider) Call(ctx context.Context, req llm.Request, trace *llm.Trace) (*anthropic.Message, llm.CallRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	text := req.Messages[0].Content[0].OfText.Text
	rec := llm.CallRecord{Model: llm.ModelHaiku45, Tier: req.Tier, Purpose: req.Purpose}
	var msg *anthropic.Message
	var err error
	switch req.Purpose {
	case "route-classifier":
		l, ok := p.routes[strings.TrimPrefix(text, "Question: ")]
		if !ok {
			return nil, rec, fmt.Errorf("no recorded route for %q", text)
		}
		rec.Usage = l.RouteTokens
		rec.CostUSD = l.RouteCostUSD
		msg, err = replayToolMessage(router.ToolClassify, map[string]string{"label": l.RouteLabel, "reason": l.RouteReason})
	case "verifier":
		q := strings.TrimPrefix(text, "Question:\n")
		q = q[:strings.Index(q, "\n\nSubmitted SQL:\n")]
		n := p.used[q]
		p.used[q] = n + 1
		list := p.verdicts[q]
		if n >= len(list) {
			return nil, rec, fmt.Errorf("no recorded verdict %d for %q", n, q)
		}
		a := list[n]
		rec.Usage = a.VerifyTokens
		rec.CostUSD = a.VerifyCostUSD
		msg, err = replayToolMessage(router.ToolVerdict, map[string]string{"verdict": a.Verdict, "reason": a.VerdictReason})
	default:
		return nil, rec, fmt.Errorf("unexpected purpose %q", req.Purpose)
	}
	if err != nil {
		return nil, rec, err
	}
	trace.Append(rec)
	return msg, rec, nil
}

func replayToolMessage(name string, input any) (*anthropic.Message, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	body := fmt.Sprintf(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5","stop_reason":"tool_use","content":[{"type":"tool_use","id":"tu_1","name":%q,"input":%s}],"usage":{"input_tokens":1,"output_tokens":1}}`, name, raw)
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

type replayAgent struct {
	results map[string]map[llm.Tier]agent.Result
}

func (a *replayAgent) Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error) {
	res, ok := a.results[question][tier]
	if !ok {
		return agent.Result{}, fmt.Errorf("no recorded %s run for %q", tier, question)
	}
	return res, nil
}

func replayResult(q Question, tier llm.Tier, model string) agent.Result {
	return agent.Result{Question: q.Text, Tier: tier, Records: []llm.CallRecord{{Model: model, Tier: tier}}}
}

func loadReplay(t *testing.T, config string) ([]Question, map[string]any, []Line) {
	t.Helper()
	dir := filepath.Join("..", "..", "bench")
	qs, err := Load(filepath.Join(dir, QuestionsFile))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := LoadSnapshot(filepath.Join(dir, AnswersFile))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := ExpectedByID(entries, qs)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := ReadLines(filepath.Join("..", "..", ResultsDirName, config, replayRuns[config]))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(qs) {
		t.Fatalf("%s: %d recorded lines for %d questions", config, len(lines), len(qs))
	}
	return qs, expected, lines
}

func replayRunner(t *testing.T, config string) (*Runner, *replayProvider, []Line) {
	t.Helper()
	qs, expected, recorded := loadReplay(t, config)
	byID := map[string]Question{}
	for _, q := range qs {
		byID[q.ID] = q
	}
	p := &replayProvider{routes: map[string]Line{}, verdicts: map[string][]Attempt{}, used: map[string]int{}}
	ag := &replayAgent{results: map[string]map[llm.Tier]agent.Result{}}
	for _, l := range recorded {
		q := byID[l.ID]
		ag.results[q.Text] = map[llm.Tier]agent.Result{}
		if len(l.Attempts) == 0 {
			p.routes[q.Text] = l
			res := replayResult(q, l.Tier, l.Model)
			res.Answer, res.SQL, res.Confidence = l.Answer, l.SQL, l.Confidence
			res.Submitted, res.Failure, res.Turns, res.SQLErrors = l.Submitted, l.Failure, l.Turns, l.SQLErrors
			res.Usage, res.CostUSD = l.Tokens, l.CostUSD-l.RouteCostUSD
			ag.results[q.Text][l.Tier] = res
			continue
		}
		for i, a := range l.Attempts {
			res := replayResult(q, a.Tier, a.Model)
			res.Answer, res.Confidence, res.Submitted = a.Answer, a.Confidence, a.Submitted
			res.Turns, res.SQLErrors, res.Usage, res.CostUSD = a.Turns, a.SQLErrors, a.Tokens, a.CostUSD
			if i == len(l.Attempts)-1 {
				res.SQL, res.Failure = l.SQL, l.Failure
			}
			ag.results[q.Text][a.Tier] = res
			if a.Verdict != "" {
				p.verdicts[q.Text] = append(p.verdicts[q.Text], a)
			}
		}
	}
	r := &Runner{Agent: ag, Questions: qs, Expected: expected, Config: config, Provider: llm.ProviderAnthropic, Repeat: 1, Concurrency: 1}
	if router.IsCascade(config) {
		tier, _ := router.CascadeVerifierTier(config)
		r.Cascade = router.NewCascade(config, ag, router.NewVerifier(p, tier, replaySemantic, nil))
	} else {
		r.Router = router.NewClassifier(p, replaySemantic)
	}
	return r, p, recorded
}

func zeroLatency(lines []Line) []Line {
	out := make([]Line, len(lines))
	for i, l := range lines {
		l.LatencyMS = 0
		l.RouteLatency = 0
		l.Attempts = append([]Attempt(nil), l.Attempts...)
		for j := range l.Attempts {
			l.Attempts[j].LatencyMS = 0
		}
		out[i] = l
	}
	return out
}

func decisionView(l Line) Line {
	v := Line{
		ID:            l.ID,
		Tier:          l.Tier,
		Model:         l.Model,
		Correct:       l.Correct,
		CorrectStrict: l.CorrectStrict,
		Answer:        l.Answer,
		SQL:           l.SQL,
		Confidence:    l.Confidence,
		Submitted:     l.Submitted,
		Turns:         l.Turns,
		SQLErrors:     l.SQLErrors,
		Tokens:        l.Tokens,
		RouteLabel:    l.RouteLabel,
		RouteReason:   l.RouteReason,
		RouteCostUSD:  l.RouteCostUSD,
		RouteTokens:   l.RouteTokens,
	}
	for _, a := range l.Attempts {
		a.LatencyMS = 0
		a.Correct = false
		v.Attempts = append(v.Attempts, a)
	}
	return v
}

func sha(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestDeciderReplayMatchesRecordedRuns(t *testing.T) {
	var golden []string
	for _, config := range []string{router.NameClassifier, router.NameCascadeVerifyHaiku} {
		r, p, recorded := replayRunner(t, config)
		lines, err := r.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for i, l := range lines {
			want := recorded[i]
			if l.Error != "" {
				t.Errorf("%s %s: error %s", config, l.ID, l.Error)
			}
			if got, exp := decisionView(l), decisionView(want); !reflect.DeepEqual(got, exp) {
				t.Errorf("%s %s: replay differs from recorded run\n got %+v\nwant %+v", config, l.ID, got, exp)
			}
			if l.FailReason != want.FailReason || l.FormatReason != want.FormatReason {
				t.Errorf("%s %s: reasons %q %q, regraded file has %q %q", config, l.ID, l.FailReason, l.FormatReason, want.FailReason, want.FormatReason)
			}
			if math.Abs(l.CostUSD-want.CostUSD) > 1e-12 {
				t.Errorf("%s %s: cost %v, recorded %v", config, l.ID, l.CostUSD, want.CostUSD)
			}
		}
		s := Summarize(config, "replay", "replay", 1, zeroLatency(lines))
		golden = append(golden, fmt.Sprintf("%s calls=%d requests=%s lines=%s summary=%s", config, len(p.reqs), sha(t, p.reqs), sha(t, zeroLatency(lines)), sha(t, s)))
	}
	got := strings.Join(golden, "\n") + "\n"
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(replayGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(replayGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(replayGoldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("decider requests or results changed\n got %s\nwant %s", got, want)
	}
}
