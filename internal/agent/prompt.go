package agent

import (
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

const (
	ToolRunSQL       = "run_sql"
	ToolSubmitAnswer = "submit_answer"
)

const Instructions = `You are a data analyst agent. You answer one question about the ecommerce database described in the next block. You work only through two tools.

How to work
1. Read the question and pick the tables, metric definitions, and conventions that apply. Use the metric SQL from the Metrics section verbatim when the question asks for that metric.
2. Call run_sql with one read-only SELECT or WITH statement. Do not end the statement with a semicolon. Results come back as CSV with a row count and a capped flag. At most 100 rows are returned; if capped is true, aggregate or add ORDER BY and LIMIT instead of reading raw rows.
3. If run_sql returns an error, read the Postgres error text, fix the statement, and try again.
4. Once the result answers the question, call submit_answer. Do not ask the user clarifying questions; pick the most reasonable reading of the question and state it through the SQL you submit.

How to answer
- Always finish by calling submit_answer. Never finish with a prose reply.
- The answer field holds only the value: a single number, a single string, or a short list. No sentences, units, or explanations.
- Write numbers as plain digits without thousands separators or currency symbols, for example 1234.56. Keep the precision the query returned unless the question asks for rounding.
- For a list, put one item per line in the order the question implies. When each item has a label and a value, write label: value.
- The sql field holds the exact statement whose result produced the answer.
- confidence is high when the query directly computes what was asked, medium when you had to interpret the question, and low when you could not verify the result.`

const InstructionsSQLite = `You are a data analyst agent. You answer one question about the SQLite database described in the next block. You work only through two tools.

How to work
1. Read the question and pick the tables, column values, and conventions that apply.
2. Call run_sql with one read-only SELECT or WITH statement. Do not end the statement with a semicolon. Results come back as CSV with a row count and a capped flag. At most 100 rows are returned; if capped is true, aggregate or add ORDER BY and LIMIT instead of reading raw rows.
3. If run_sql returns an error, read the SQLite error text, fix the statement, and try again.
4. Once the result answers the question, call submit_answer. Do not ask the user clarifying questions; pick the most reasonable reading of the question and state it through the SQL you submit.

How to answer
- Always finish by calling submit_answer. Never finish with a prose reply.
- The answer field holds only the value: a single number, a single string, or a short list. No sentences, units, or explanations.
- Write numbers as plain digits without thousands separators or currency symbols, for example 1234.56. Keep the precision the query returned unless the question asks for rounding.
- For a list, put one item per line in the order the question implies. When each item has a label and a value, write label: value.
- The sql field holds the exact statement whose result produced the answer.
- confidence is high when the query directly computes what was asked, medium when you had to interpret the question, and low when you could not verify the result.`

const NudgeText = "You have not called submit_answer. Call submit_answer now with the best answer you have. Do not reply in prose."

const PrefixTargetApproxTokens = 4800

var instructionPaddingSentences = []string{
	"Work carefully and check each statement against the schema before running it.",
	"Read the question twice and confirm the result answers what was asked.",
	"Keep queries simple and verify every column name before submitting.",
	"When a query fails, read the error text and fix one thing at a time.",
}

func prefixApproxTokens(text string) int {
	if text == "" {
		return 0
	}
	if n := len(text) / 4; n > 0 {
		return n
	}
	return 1
}

func InstructionsFor(semanticText string) string {
	return padInstructions(Instructions, semanticText)
}

func InstructionsSQLiteFor(semanticText string) string {
	return padInstructions(InstructionsSQLite, semanticText)
}

func padInstructions(base, semanticText string) string {
	if prefixApproxTokens(base+semanticText) >= PrefixTargetApproxTokens {
		return base
	}
	var sentences []string
	for i := 0; ; i++ {
		sentences = append(sentences, instructionPaddingSentences[i%len(instructionPaddingSentences)])
		if prefixApproxTokens(base+joinPadding(sentences)+semanticText) >= PrefixTargetApproxTokens {
			break
		}
	}
	return base + joinPadding(sentences)
}

func EvidenceBlock(evidence string) string {
	return "\n\nContext\n" + strings.TrimSpace(evidence)
}

func joinPadding(sentences []string) string {
	if len(sentences) == 0 {
		return ""
	}
	return "\n\n" + strings.Join(sentences, " ")
}

func SystemBlocks(semanticText string) []anthropic.TextBlockParam {
	return SystemBlocksWith(Instructions, semanticText)
}

func SystemBlocksNoCache(semanticText string) []anthropic.TextBlockParam {
	return SystemBlocksNoCacheWith(Instructions, semanticText)
}

func SystemBlocksWith(instructions, semanticText string) []anthropic.TextBlockParam {
	return []anthropic.TextBlockParam{
		{Text: instructions},
		{Text: semanticText, CacheControl: anthropic.NewCacheControlEphemeralParam()},
	}
}

func SystemBlocksNoCacheWith(instructions, semanticText string) []anthropic.TextBlockParam {
	return []anthropic.TextBlockParam{
		{Text: instructions},
		{Text: semanticText},
	}
}

func Tools() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		runSQLTool("Run one read-only SELECT or WITH statement against the ecommerce Postgres database. Returns the row count, whether the result was capped at 100 rows, and the rows as CSV with a header line. On failure returns the Postgres error text."),
		submitAnswerTool(),
	}
}

func ToolsSQLite() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		runSQLTool("Run one read-only SELECT or WITH statement against the SQLite database. Returns the row count, whether the result was capped at 100 rows, and the rows as CSV with a header line. On failure returns the SQLite error text."),
		submitAnswerTool(),
	}
}

func runSQLTool(description string) anthropic.ToolUnionParam {
	return anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
		Name:        ToolRunSQL,
		Description: anthropic.String(description),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"sql": map[string]any{
					"type":        "string",
					"description": "A single SELECT or WITH statement without a trailing semicolon.",
				},
			},
			Required:    []string{"sql"},
			ExtraFields: map[string]any{"additionalProperties": false},
		},
	}}
}

func submitAnswerTool() anthropic.ToolUnionParam {
	return anthropic.ToolUnionParam{OfTool: &anthropic.ToolParam{
		Name:        ToolSubmitAnswer,
		Description: anthropic.String("Submit the final answer. Call this exactly once, when you are done. The answer is a single value or a short list, never prose."),
		Strict:      anthropic.Bool(true),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"answer": map[string]any{
					"type":        "string",
					"description": "The value only: one number, one string, or a short list with one item per line.",
				},
				"sql": map[string]any{
					"type":        "string",
					"description": "The exact SQL statement whose result produced the answer.",
				},
				"confidence": map[string]any{
					"type": "string",
					"enum": []string{"high", "medium", "low"},
				},
			},
			Required:    []string{"answer", "sql", "confidence"},
			ExtraFields: map[string]any{"additionalProperties": false},
		},
	}}
}
