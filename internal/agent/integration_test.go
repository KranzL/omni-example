package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/llm"
	"github.com/KranzL/omni-example/internal/semantic"
)

func TestIntegrationCheapTierReadsCache(t *testing.T) {
	if os.Getenv("OMNI_INTEGRATION") != "1" {
		t.Skip("OMNI_INTEGRATION=1 is required for paid integration tests")
	}
	key := os.Getenv("ANTHROPIC_API_KEY")
	dsn := os.Getenv("DATABASE_URL")
	if key == "" || dsn == "" {
		t.Skip("ANTHROPIC_API_KEY and DATABASE_URL are required")
	}
	path, err := semantic.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	sem, err := semantic.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	a := New(llm.NewClient(key, os.Getenv("ANTHROPIC_WORKSPACE_ID")), PoolQuerier{Pool: pool}, sem.Render())

	const question = "How many distribution centers are there?"
	var runs []Result
	for i := 0; i < 2; i++ {
		res, err := a.Run(ctx, question, llm.TierCheap)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("run %d: answer=%q turns=%d usage=%+v cost=$%.6f", i+1, res.Answer, res.Turns, res.Usage, res.CostUSD)
		runs = append(runs, res)
	}
	if !runs[1].Submitted || runs[1].Answer != "10" {
		t.Errorf("second run answer %q submitted=%v", runs[1].Answer, runs[1].Submitted)
	}
	first := runs[1].Records[0].Usage
	if first.CacheReadInputTokens == 0 {
		t.Fatalf("second run first call did not read from cache: %+v", first)
	}
}
