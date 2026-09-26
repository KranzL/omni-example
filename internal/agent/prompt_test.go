package agent

import (
	"fmt"
	"strings"
	"testing"
)

func TestInstructionsForKeepsBaseOnLongPrefix(t *testing.T) {
	long := strings.Repeat("x", 20000)
	if got := InstructionsFor(long); got != Instructions {
		t.Error("long prefix pads the instructions but must leave them unchanged")
	}
}

func TestInstructionsForPadsShortPrefix(t *testing.T) {
	first := InstructionsFor("tiny semantic text")
	second := InstructionsFor("tiny semantic text")
	if first != second {
		t.Fatal("two padded instructions differ")
	}
	if !strings.HasPrefix(first, Instructions) {
		t.Fatal("padded instructions do not start with the base text")
	}
	if first == Instructions {
		t.Fatal("short prefix leaves the instructions unchanged")
	}
	if got := prefixApproxTokens(first + "tiny semantic text"); got < PrefixTargetApproxTokens {
		t.Fatalf("padded prefix = %d approx tokens, want at least %d", got, PrefixTargetApproxTokens)
	}
}

func TestInstructionsPaddingHasNoFacts(t *testing.T) {
	padding := strings.TrimPrefix(InstructionsFor("tiny"), Instructions)
	if padding == "" {
		t.Fatal("short prefix adds no padding")
	}
	for _, leak := range []string{
		"Cancelled", "Complete", "Returned", "Processing", "Shipped",
		"order_items", "inventory_items", "session_id", "distribution_centers",
		"revenue", "margin", "gross", "metric", "synonym", "join path",
		"allowed values", "percentile", "median", "cohort",
		"WHERE", "JOIN", "GROUP BY", "SELECT", "2024", "2023",
	} {
		if strings.Contains(padding, leak) {
			t.Errorf("padding contains %q", leak)
		}
	}
}

func TestNewPrefixSystem(t *testing.T) {
	a := NewPrefix(nil, nil, "tiny semantic text")
	if len(a.System) != 2 {
		t.Fatalf("len(System) = %d, want 2", len(a.System))
	}
	if a.System[0].Text != InstructionsFor("tiny semantic text") {
		t.Error("NewPrefix does not use the padded instructions")
	}
	if a.System[1].Text != "tiny semantic text" {
		t.Error("NewPrefix drops the semantic text")
	}
	if a.NoCache {
		t.Error("NewPrefix sets NoCache")
	}
	n := NewPrefixNoCache(nil, nil, "tiny semantic text")
	if !n.NoCache {
		t.Error("NewPrefixNoCache leaves NoCache false")
	}
	if n.System[0].Text != InstructionsFor("tiny semantic text") {
		t.Error("NewPrefixNoCache does not use the padded instructions")
	}
}

func TestEvidenceBlock(t *testing.T) {
	got := EvidenceBlock("ratio means a / b")
	if got != "\n\nContext\nratio means a / b" {
		t.Fatalf("block = %q", got)
	}
}

func TestInstructionsSQLiteMentionsSQLite(t *testing.T) {
	for _, want := range []string{"SQLite", "submit_answer", "run_sql"} {
		if !strings.Contains(InstructionsSQLite, want) {
			t.Fatalf("SQLite instructions miss %q", want)
		}
	}
	for _, leak := range []string{"Postgres", "ecommerce", "metric SQL"} {
		if strings.Contains(InstructionsSQLite, leak) {
			t.Fatalf("SQLite instructions leak %q", leak)
		}
	}
}

func TestInstructionsSQLiteForPadsShortPrefix(t *testing.T) {
	got := InstructionsSQLiteFor("tiny semantic text")
	if !strings.HasPrefix(got, InstructionsSQLite) {
		t.Fatal("padded SQLite instructions do not start with the base text")
	}
	if got == InstructionsSQLite {
		t.Fatal("short prefix leaves the SQLite instructions unchanged")
	}
	if n := prefixApproxTokens(got + "tiny semantic text"); n < PrefixTargetApproxTokens {
		t.Fatalf("padded prefix = %d approx tokens, want at least %d", n, PrefixTargetApproxTokens)
	}
}

func TestToolsSQLiteRunSQLMentionsSQLite(t *testing.T) {
	tools := ToolsSQLite()
	if len(tools) != 2 || tools[0].OfTool.Name != ToolRunSQL || tools[1].OfTool.Name != ToolSubmitAnswer {
		t.Fatalf("tools = %+v", tools)
	}
	desc := fmt.Sprint(tools[0].OfTool.Description.Value)
	if !strings.Contains(desc, "SQLite") || strings.Contains(desc, "Postgres") {
		t.Fatalf("run_sql description = %q", desc)
	}
}
