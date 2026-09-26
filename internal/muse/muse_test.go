package muse

import (
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/agent"
)

func toolResultLine(command string, exitCode int, output string) string {
	inner := `{"chunk_id":"exec-1-1","command":` + quoteJSON(command) + `,"exit_code":` + itoa(exitCode) + `,"output":` + quoteJSON(output) + `}`
	return `{"payload_type":"tool.result","payload":{"text":` + quoteJSON(inner) + `}}`
}

func terminalLine(terminal, text string) string {
	return `{"payload_type":"run.terminal.completed","payload":{"terminal":"` + terminal + `","text":` + quoteJSON(text) + `}}`
}

func quoteJSON(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func TestParseTranscriptSuccessfulRun(t *testing.T) {
	lines := []string{
		`{"payload_type":"run.model.configured","payload":{"model_id":"muse-spark-1.3-contributor"}}`,
		toolResultLine(`./harness sql "SELECT email FROM public.users WHERE id = 123"`, 0, "email\njgarcia@gmail.com\n"),
		terminalLine("completed", `{"answer": "jgarcia@gmail.com", "confidence": "high", "sql": "SELECT email FROM public.users WHERE id = 123"}`),
	}
	res := agent.Result{}
	parseTranscript([]byte(strings.Join(lines, "\n")), &res)

	if res.Answer != "jgarcia@gmail.com" {
		t.Errorf("answer = %q", res.Answer)
	}
	if res.SQL != "SELECT email FROM public.users WHERE id = 123" {
		t.Errorf("sql = %q", res.SQL)
	}
	if res.Confidence != "high" {
		t.Errorf("confidence = %q", res.Confidence)
	}
	if !res.Submitted {
		t.Error("submitted = false, want true")
	}
	if res.Turns != 1 {
		t.Errorf("turns = %d, want 1", res.Turns)
	}
	if res.SQLErrors != 0 {
		t.Errorf("sql_errors = %d, want 0", res.SQLErrors)
	}
	if len(res.Steps) != 1 || res.Steps[0].SQL != "SELECT email FROM public.users WHERE id = 123" {
		t.Errorf("steps = %+v", res.Steps)
	}
	if res.Failure != "" {
		t.Errorf("failure = %q, want empty", res.Failure)
	}
}

func TestParseTranscriptGuardRejection(t *testing.T) {
	lines := []string{
		toolResultLine(`./harness sql "UPDATE users SET email = 'x'"`, 1, "forbidden keyword: UPDATE"),
		toolResultLine(`./harness sql "SELECT email FROM public.users WHERE id = 123"`, 0, "email\njgarcia@gmail.com\n"),
		terminalLine("completed", `{"answer": "jgarcia@gmail.com", "confidence": "high", "sql": "SELECT email FROM public.users WHERE id = 123"}`),
	}
	res := agent.Result{}
	parseTranscript([]byte(strings.Join(lines, "\n")), &res)

	if res.SQLErrors != 1 {
		t.Errorf("sql_errors = %d, want 1", res.SQLErrors)
	}
	if res.Turns != 2 {
		t.Errorf("turns = %d, want 2", res.Turns)
	}
	if !res.Submitted || res.Answer != "jgarcia@gmail.com" {
		t.Errorf("submitted=%v answer=%q", res.Submitted, res.Answer)
	}
}

func TestParseTranscriptStepCapFailure(t *testing.T) {
	lines := []string{
		toolResultLine(`./harness sql "SELECT 1"`, 0, "1\n1\n"),
	}
	res := agent.Result{}
	parseTranscript([]byte(strings.Join(lines, "\n")), &res)

	if res.Submitted {
		t.Error("submitted = true, want false: no terminal event was ever emitted")
	}
	if res.Answer != "" {
		t.Errorf("answer = %q, want empty", res.Answer)
	}
}

func TestParseTranscriptNonSchemaFinalAnswer(t *testing.T) {
	lines := []string{
		terminalLine("completed", "the answer is 42"),
	}
	res := agent.Result{}
	parseTranscript([]byte(strings.Join(lines, "\n")), &res)

	if res.Submitted {
		t.Error("submitted = true, want false: final text was not schema JSON")
	}
	if res.Answer != "the answer is 42" {
		t.Errorf("answer = %q, want the raw text kept as a fallback", res.Answer)
	}
	if !strings.Contains(res.Failure, "not schema JSON") {
		t.Errorf("failure = %q, want it to name the parse problem", res.Failure)
	}
}

func TestParseTranscriptIgnoresNonSQLToolCalls(t *testing.T) {
	lines := []string{
		`{"payload_type":"tool.result","payload":{"text":"{\"command\":\"ls -la\",\"exit_code\":1,\"output\":\"denied\"}"}}`,
		toolResultLine(`./harness sql "SELECT 1"`, 0, "1\n1\n"),
		terminalLine("completed", `{"answer": "1", "confidence": "high", "sql": "SELECT 1"}`),
	}
	res := agent.Result{}
	parseTranscript([]byte(strings.Join(lines, "\n")), &res)

	if res.SQLErrors != 0 {
		t.Errorf("sql_errors = %d, want 0: the failing ls call is not a SQL step", res.SQLErrors)
	}
	if len(res.Steps) != 1 {
		t.Errorf("steps = %d, want 1", len(res.Steps))
	}
}

func TestExtractSQL(t *testing.T) {
	got := extractSQL(`./harness sql "SELECT email FROM public.users WHERE id = 123"`)
	want := "SELECT email FROM public.users WHERE id = 123"
	if got != want {
		t.Errorf("extractSQL = %q, want %q", got, want)
	}
	if got := extractSQL("ls -la"); got != "ls -la" {
		t.Errorf("extractSQL of a non-sql command = %q, want it returned unchanged", got)
	}
}

func TestAppendFailure(t *testing.T) {
	if got := appendFailure("", "a"); got != "a" {
		t.Errorf("appendFailure empty = %q", got)
	}
	if got := appendFailure("a", "b"); got != "a; b" {
		t.Errorf("appendFailure non-empty = %q", got)
	}
}

func TestBuildPromptContainsGuardInstructions(t *testing.T) {
	p := buildPrompt("SEMANTIC TEXT", "What is the email of user 123?")
	if !strings.Contains(p, "./harness sql") {
		t.Error("prompt does not mention the guarded sql command")
	}
	if !strings.Contains(p, "Do not use psql") {
		t.Error("prompt does not forbid other database access methods")
	}
	if !strings.Contains(p, "SEMANTIC TEXT") || !strings.Contains(p, "What is the email of user 123?") {
		t.Error("prompt does not include the semantic layer and question")
	}
}
