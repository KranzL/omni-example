package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KranzL/omni-example/internal/agent"
	"github.com/KranzL/omni-example/internal/semantic"
)

func TestParseAskContextLevels(t *testing.T) {
	args, err := parseAskArgs([]string{"what is gross revenue?"})
	if err != nil {
		t.Fatal(err)
	}
	if !args.levels.IsAll() {
		t.Fatalf("default levels = %v, want all", args.levels)
	}
	args, err = parseAskArgs([]string{"q", "--context-levels", "model"})
	if err != nil {
		t.Fatal(err)
	}
	if args.levels != (semantic.Levels{Model: true}) {
		t.Fatalf("levels = %v, want model only", args.levels)
	}
	args, err = parseAskArgs([]string{"q", "--context-levels=model,topic"})
	if err != nil {
		t.Fatal(err)
	}
	if args.levels != (semantic.Levels{Model: true, Topic: true}) {
		t.Fatalf("levels = %v, want model plus topic", args.levels)
	}
	if _, err := parseAskArgs([]string{"q", "--context-levels=foo"}); err == nil {
		t.Fatal("bad levels: want error")
	}
	if !strings.Contains(askUsage, "--context-levels") {
		t.Fatalf("usage %q does not mention --context-levels", askUsage)
	}
}

func TestAskRunsSavesCompletedRunsOnFailure(t *testing.T) {
	var saved []agent.Result
	err := askRuns(3, func(i int) (agent.Result, error) {
		if i == 2 {
			return agent.Result{Answer: "partial"}, errors.New("boom")
		}
		return agent.Result{Answer: "ok"}, nil
	}, func(r []agent.Result) error {
		saved = r
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(saved) != 1 || saved[0].Answer != "ok" {
		t.Fatalf("saved = %+v, want the one completed run", saved)
	}
	saved = nil
	if err := askRuns(1, func(int) (agent.Result, error) { return agent.Result{}, errors.New("boom") }, func(r []agent.Result) error {
		saved = r
		return nil
	}); err == nil || saved != nil {
		t.Fatalf("first run fails: err=%v saved=%v, want error and no write", err, saved)
	}
}

func TestWriteAskResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.result.json")
	if err := writeAskResults(path, []agent.Result{{Answer: "42"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "42") {
		t.Fatalf("file = %s %v", data, err)
	}
}

func TestAskRunsErrorsWhenNothingSubmitted(t *testing.T) {
	saved := 0
	save := func(r []agent.Result) error {
		saved = len(r)
		return nil
	}
	err := askRuns(2, func(int) (agent.Result, error) { return agent.Result{Failure: "max turns"}, nil }, save)
	if err == nil || !strings.Contains(err.Error(), "did not submit") || saved != 2 {
		t.Fatalf("no submit: err=%v saved=%d, want error after saving both runs", err, saved)
	}
	err = askRuns(2, func(i int) (agent.Result, error) { return agent.Result{Submitted: i == 2}, nil }, save)
	if err != nil {
		t.Fatalf("one submitted run: %v", err)
	}
}
