package bench

import (
	"fmt"
	"strings"

	"github.com/KranzL/omni-example/internal/llm"
)

var confusionRows = []string{DifficultyEasy, DifficultyModerate, DifficultyHard, DifficultyExpert, DifficultySimple, DifficultyChallenging}

func ConfusionTable(confusion map[string]map[llm.Tier]int) string {
	var b strings.Builder
	b.WriteString("| labelled \\ routed |")
	for _, t := range llm.Tiers {
		fmt.Fprintf(&b, " %s |", t)
	}
	b.WriteString("\n|---|")
	for range llm.Tiers {
		b.WriteString("---:|")
	}
	b.WriteString("\n")
	for _, d := range confusionRows {
		row, ok := confusion[d]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "| %s |", d)
		for _, t := range llm.Tiers {
			fmt.Fprintf(&b, " %d |", row[t])
		}
		b.WriteString("\n")
	}
	return b.String()
}
