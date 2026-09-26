package main

import (
	"os"
	"path/filepath"

	"github.com/KranzL/omni-example/internal/semantic"
)

func main() {
	out := os.Args[1]
	for _, db := range []string{"superhero", "thrombosis_prediction", "toxicology"} {
		sem, err := semantic.Load(filepath.Join("bench/bird/semantic", db+".yaml"))
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(out, "semantic_bird_"+db+".txt"), []byte(sem.Render()), 0o644); err != nil {
			panic(err)
		}
	}
}
