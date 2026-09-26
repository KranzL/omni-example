package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/report"
)

func reportCmd(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: harness report")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := bench.FindBenchDir(cwd)
	if dir == "" {
		return fmt.Errorf("bench/questions.yaml not found")
	}
	root := filepath.Dir(dir)
	resultsDir := filepath.Join(root, bench.ResultsDirName)
	paths, err := report.FindLatest(resultsDir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no latest.json under %s", resultsDir)
	}
	cs, err := report.Load(root, paths)
	if err != nil {
		return err
	}
	if err := report.Write(resultsDir, report.Generate(cs)); err != nil {
		return err
	}
	readmePath := filepath.Join(root, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return err
	}
	updated, err := report.EmbedReadme(string(readme), report.ReadmeSection(cs))
	if err != nil {
		return err
	}
	if err := os.WriteFile(readmePath, []byte(updated), 0o644); err != nil {
		return err
	}
	fmt.Printf("read %d configurations; wrote %s, %s, %s and updated %s\n", len(cs),
		filepath.Join(bench.ResultsDirName, report.SummaryFile), filepath.Join(bench.ResultsDirName, report.ParetoFile),
		filepath.Join(bench.ResultsDirName, report.DifficultyFile), "README.md")
	fmt.Print(report.Headline(cs))
	return nil
}
