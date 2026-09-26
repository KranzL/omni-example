package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/db"
	"github.com/KranzL/omni-example/internal/semantic"
)

const birdUsage = `usage: harness bird gen-layers | harness bird validate`

func birdCmd(args []string) error {
	if len(args) == 1 && args[0] == "gen-layers" {
		return birdGenLayers()
	}
	if len(args) == 1 && args[0] == "validate" {
		return birdValidate()
	}
	return fmt.Errorf("%s", birdUsage)
}

func birdDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	base := bench.FindBenchDir(cwd)
	if base == "" {
		return "", fmt.Errorf("bench/questions.yaml not found")
	}
	return filepath.Join(base, bench.BirdDirName), nil
}

func birdGenLayers() error {
	dir, err := birdDir()
	if err != nil {
		return err
	}
	dbDir := filepath.Join(dir, bench.BirdDBDir)
	outDir := filepath.Join(dir, bench.BirdSemanticDir)
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	generated := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sqlite") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".sqlite")
		sqldb, err := db.OpenSQLite(filepath.Join(dbDir, e.Name()))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		sem, err := semantic.GenerateSQLite(sqldb, semantic.GenOptions{
			Schema:  "main",
			Dataset: fmt.Sprintf("BIRD Mini-Dev %s database (SQLite)", name),
			Title:   fmt.Sprintf("the BIRD Mini-Dev %s SQLite database", name),
		})
		sqldb.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		data, err := sem.ToYAML()
		if err != nil {
			return err
		}
		out := filepath.Join(outDir, name+".yaml")
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		cols := 0
		for _, t := range sem.Tables {
			cols += len(t.Columns)
		}
		fmt.Printf("%s: tables=%d columns=%d enums=%d paths=%d wrote %s\n", name, len(sem.Tables), cols, len(sem.Enums), len(sem.Paths), out)
		generated++
	}
	if generated == 0 {
		return fmt.Errorf("no .sqlite files in %s", dbDir)
	}
	return nil
}

func birdValidate() error {
	dir, err := birdDir()
	if err != nil {
		return err
	}
	qs, err := bench.LoadBird(filepath.Join(dir, bench.BirdQuestionsFile))
	if err != nil {
		return err
	}
	dbs := map[string]*db.SQLiteBackend{}
	closeAll := func() {
		for _, b := range dbs {
			b.DB.Close()
		}
	}
	defer closeAll()
	backendFor := func(dbID string) (*db.SQLiteBackend, error) {
		if b, ok := dbs[dbID]; ok {
			return b, nil
		}
		sqldb, err := db.OpenSQLite(filepath.Join(dir, bench.BirdDBDir, dbID+".sqlite"))
		if err != nil {
			return nil, err
		}
		b := &db.SQLiteBackend{DB: sqldb}
		dbs[dbID] = b
		return b, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	computedAt := time.Now().UTC().Format(time.RFC3339)
	entries := make([]bench.SnapshotEntry, 0, len(qs))
	failed := false
	for _, q := range qs {
		backend, err := backendFor(q.DbID)
		if err != nil {
			fmt.Printf("%s ERROR: %s\n", q.ID, err)
			failed = true
			continue
		}
		start := time.Now()
		res, err := backend.Query(ctx, q.SQL, bench.MaxAnswerRows)
		elapsed := time.Since(start)
		if err != nil {
			fmt.Printf("%s ERROR after %.2fs: %s\n", q.ID, elapsed.Seconds(), err)
			failed = true
			continue
		}
		if res.Truncated {
			fmt.Printf("%s ERROR after %.2fs: answer exceeds %d rows\n", q.ID, elapsed.Seconds(), bench.MaxAnswerRows)
			failed = true
			continue
		}
		answer, err := bench.BuildAnswer(q, res)
		if err != nil {
			fmt.Printf("%s ERROR after %.2fs: %s\n", q.ID, elapsed.Seconds(), err)
			failed = true
			continue
		}
		entries = append(entries, bench.SnapshotEntry{ID: q.ID, Answer: answer, ComputedAt: computedAt})
		fmt.Printf("== %s (%s %s) %.2fs\n%s", q.ID, q.DbID, q.Difficulty, elapsed.Seconds(), bench.FormatAnswer(answer))
	}
	if failed {
		return fmt.Errorf("bird validate failed")
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	out := filepath.Join(dir, bench.BirdAnswersFile)
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s with %d answers\n", out, len(entries))
	return nil
}
