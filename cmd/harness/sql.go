package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/config"
	"github.com/KranzL/omni-example/internal/db"
)

const sqlUsage = `usage: harness sql "SELECT ..."`

func sqlCmd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s", sqlUsage)
	}
	stmt := args[0]
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("missing DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	res, err := db.Query(ctx, pool, stmt, bench.MaxAnswerRows)
	if err != nil {
		return err
	}
	fmt.Print(res.CSV())
	if res.Truncated {
		fmt.Fprintf(os.Stderr, "truncated at %d rows\n", bench.MaxAnswerRows)
	}
	return nil
}
