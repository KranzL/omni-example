package muse

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/llm"
)

const (
	Binary          = "muse"
	sqlCommandLabel = "harness sql"
)

type Agent struct {
	WorkspaceDir string
	SemanticText string
	DatabaseURL  string
	MaxSteps     int
	Timeout      time.Duration
}

func New(workspaceDir, semanticText, databaseURL string) *Agent {
	return &Agent{
		WorkspaceDir: workspaceDir,
		SemanticText: semanticText,
		DatabaseURL:  databaseURL,
		MaxSteps:     24,
		Timeout:      5 * time.Minute,
	}
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func buildPrompt(semanticText, question string) string {
	var b strings.Builder
	b.WriteString("You are answering a data question about an ecommerce database. Below is the semantic layer describing the schema, metrics, and conventions.\n\n")
	b.WriteString("The ONLY way you may query the database is by running this exact shell command, substituting your SQL:\n\n")
	b.WriteString("./harness sql \"SELECT ...\"\n\n")
	b.WriteString("Do not use psql, any other database client, or any other method to access the database. You do not have and do not need any database credentials directly; the harness binary handles the connection itself. The command enforces a read-only guard: only SELECT/WITH statements are allowed, results are capped, and mutating statements will be rejected.\n\n")
	b.WriteString("Run as many queries as you need to answer the question with confidence, then give your final answer as a single value or short list, never prose.\n\n")
	b.WriteString("=== SEMANTIC LAYER ===\n")
	b.WriteString(semanticText)
	b.WriteString("\n\n=== QUESTION ===\n")
	b.WriteString(question)
	b.WriteString("\n")
	return b.String()
}

const outputSchema = `{
  "type": "object",
  "properties": {
    "answer": {"type": "string", "description": "The final answer, a single value or short list, no prose."},
    "sql": {"type": "string", "description": "The exact SQL query (or the last one, if several were needed) that produced the answer."},
    "confidence": {"type": "string", "enum": ["high", "medium", "low"]}
  },
  "required": ["answer", "sql", "confidence"],
  "additionalProperties": false
}
`

type finalAnswer struct {
	Answer     string `json:"answer"`
	SQL        string `json:"sql"`
	Confidence string `json:"confidence"`
}

type execEvent struct {
	PayloadType string          `json:"payload_type"`
	Payload     json.RawMessage `json:"payload"`
}

type toolCommand struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Output     string `json:"output"`
	DurationMS int64  `json:"-"`
}

type terminalPayload struct {
	Terminal string `json:"terminal"`
	Text     string `json:"text"`
	Reason   string `json:"reason"`
}

type toolResultPayload struct {
	Text string `json:"text"`
}

type usagePayload struct {
	Kind  string `json:"kind"`
	Event struct {
		Kind   string `json:"kind"`
		Record struct {
			Quantity struct {
				InputTokens     int64 `json:"input_tokens"`
				OutputTokens    int64 `json:"output_tokens"`
				CachedTokens    int64 `json:"cached_tokens"`
				ReasoningTokens int64 `json:"reasoning_tokens"`
				MainLLMSteps    int   `json:"main_llm_steps"`
			} `json:"quantity"`
		} `json:"record"`
	} `json:"event"`
}

func (a *Agent) outputSchemaPath() string {
	return filepath.Join(a.WorkspaceDir, "output-schema.json")
}

func (a *Agent) ensureOutputSchema() error {
	return os.WriteFile(a.outputSchemaPath(), []byte(outputSchema), 0o644)
}

func (a *Agent) Run(ctx context.Context, question string, tier llm.Tier) (agent.Result, error) {
	res := agent.Result{Question: question, Tier: tier}

	id, err := newID()
	if err != nil {
		return res, err
	}
	if err := a.ensureOutputSchema(); err != nil {
		return res, err
	}
	promptPath := filepath.Join(a.WorkspaceDir, "prompt-"+id+".txt")
	if err := os.WriteFile(promptPath, []byte(buildPrompt(a.SemanticText, question)), 0o644); err != nil {
		return res, err
	}
	defer os.Remove(promptPath)

	runCtx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, Binary, "exec",
		"--workspace", a.WorkspaceDir,
		"--prompt-file", promptPath,
		"--disable-write",
		"--sandbox-network", "enabled",
		"--approval-mode", "never",
		"--max-model-steps", strconv.Itoa(a.MaxSteps),
		"--session-id", id,
		"--json",
		"--output-schema", a.outputSchemaPath(),
	)
	cmd.Dir = a.WorkspaceDir
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "DATABASE_URL=" + a.DatabaseURL}
	start := time.Now()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	res.WallMS = time.Since(start).Milliseconds()

	parseTranscript(stdout.Bytes(), &res)

	if u, uerr := exportUsage(runCtx, a.WorkspaceDir, id); uerr == nil {
		res.Usage = u
		if cost, cerr := llm.Cost(llm.ModelMuseSpark13Contributor, u); cerr == nil {
			res.CostUSD = cost
		}
		res.CacheRead = u.CacheReadInputTokens
		res.CacheCreation = u.CacheCreationInputTokens
		res.Records = []llm.CallRecord{{
			Time:      start,
			Provider:  "muse",
			Model:     llm.ModelMuseSpark13Contributor,
			Tier:      tier,
			Purpose:   "agent",
			Usage:     u,
			CostUSD:   res.CostUSD,
			LatencyMS: res.WallMS,
		}}
	} else {
		res.Failure = appendFailure(res.Failure, fmt.Sprintf("usage export: %v", uerr))
	}

	if runErr != nil && !res.Submitted {
		res.Failure = appendFailure(res.Failure, fmt.Sprintf("muse exec: %v: %s", runErr, strings.TrimSpace(stderr.String())))
	}
	return res, nil
}

func appendFailure(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}

func parseTranscript(stdout []byte, res *agent.Result) {
	turn := 0
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var ev execEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		switch ev.PayloadType {
		case "tool.result":
			var tr toolResultPayload
			if err := json.Unmarshal(ev.Payload, &tr); err != nil {
				continue
			}
			var tc toolCommand
			if err := json.Unmarshal([]byte(tr.Text), &tc); err != nil {
				continue
			}
			if !strings.Contains(tc.Command, sqlCommandLabel) {
				continue
			}
			turn++
			step := agent.SQLStep{Turn: turn, SQL: extractSQL(tc.Command), Output: tc.Output}
			if tc.ExitCode != 0 {
				step.Error = strings.TrimSpace(tc.Output)
				res.SQLErrors++
			} else {
				step.Rows = strings.Count(strings.TrimRight(tc.Output, "\n"), "\n")
			}
			res.Steps = append(res.Steps, step)
		case "run.terminal.completed":
			var tp terminalPayload
			if err := json.Unmarshal(ev.Payload, &tp); err != nil {
				continue
			}
			if tp.Terminal != "completed" {
				res.Failure = appendFailure(res.Failure, "terminal: "+tp.Terminal+" "+tp.Reason)
				continue
			}
			var fa finalAnswer
			if err := json.Unmarshal([]byte(tp.Text), &fa); err != nil {
				res.Answer = tp.Text
				res.Failure = appendFailure(res.Failure, "final answer not schema JSON")
				continue
			}
			res.Answer = fa.Answer
			res.SQL = fa.SQL
			res.Confidence = fa.Confidence
			res.Submitted = true
		}
	}
	res.Turns = turn
	if res.Turns == 0 {
		res.Turns = 1
	}
}

func extractSQL(command string) string {
	i := strings.Index(command, "./harness sql ")
	if i < 0 {
		return command
	}
	rest := strings.TrimSpace(command[i+len("./harness sql "):])
	rest = strings.Trim(rest, "\"")
	return rest
}

func exportUsage(ctx context.Context, workspaceDir, sessionID string) (llm.Usage, error) {
	outPath := filepath.Join(workspaceDir, "session-export-"+sessionID+".json")
	defer os.Remove(outPath)
	cmd := exec.CommandContext(ctx, Binary, "export", "--session", sessionID, "--out", outPath)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME")}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return llm.Usage{}, fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return llm.Usage{}, err
	}
	var doc struct {
		Events []struct {
			Envelope struct {
				PayloadType string          `json:"payload_type"`
				Payload     json.RawMessage `json:"payload"`
			} `json:"envelope"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return llm.Usage{}, err
	}
	var total llm.Usage
	for _, e := range doc.Events {
		if e.Envelope.PayloadType != "runtime.session" {
			continue
		}
		var up usagePayload
		if err := json.Unmarshal(e.Envelope.Payload, &up); err != nil {
			continue
		}
		if up.Event.Kind != "goal_usage_attribution" {
			continue
		}
		q := up.Event.Record.Quantity
		total.InputTokens += q.InputTokens - q.CachedTokens
		total.OutputTokens += q.OutputTokens
		total.CacheReadInputTokens += q.CachedTokens
	}
	return total, nil
}
