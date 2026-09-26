package main

import (
	"encoding/json"
	"os"

	"github.com/KranzL/omni-example/internal/bench"
	"github.com/KranzL/omni-example/internal/router"
	"github.com/KranzL/omni-example/internal/semantic"
)

type row struct {
	ID       string          `json:"id"`
	Set      string          `json:"set"`
	Label    string          `json:"label"`
	Question string          `json:"question"`
	Features router.Features `json:"features"`
	Score    float64         `json:"score"`
}

func main() {
	out := os.Args[1]
	sem, err := semantic.Load("semantic/ecomm.yaml")
	if err != nil {
		panic(err)
	}
	h := router.NewHeuristic(sem, router.DefaultHeuristicParams())
	var rows []row
	qs, err := bench.Load("bench/questions.yaml")
	if err != nil {
		panic(err)
	}
	for _, q := range qs {
		f := h.Features(q.Text)
		rows = append(rows, row{q.ID, "bench", q.Difficulty, q.Text, f, h.Params.Score(f)})
	}
	seeds, err := router.LoadSeeds("bench/router_seed.yaml")
	if err != nil {
		panic(err)
	}
	for _, s := range seeds {
		f := h.Features(s.Question)
		rows = append(rows, row{s.ID, "seed", s.Label, s.Question, f, h.Params.Score(f)})
	}
	data, _ := json.MarshalIndent(rows, "", " ")
	if err := os.WriteFile(out+"/features.json", data, 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(out+"/semantic_ecomm.txt", []byte(sem.Render()), 0o644); err != nil {
		panic(err)
	}
}
