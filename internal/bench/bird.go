package bench

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/KranzL/omni-example/internal/db"
	"gopkg.in/yaml.v3"
)

var birdIDPattern = regexp.MustCompile(`^b\d{2}$`)

func LoadBird(path string) ([]Question, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var qs []Question
	if err := yaml.Unmarshal(data, &qs); err != nil {
		return nil, err
	}
	if err := ValidateBird(qs); err != nil {
		return nil, err
	}
	return qs, nil
}

func ValidateBird(qs []Question) error {
	if len(qs) == 0 {
		return fmt.Errorf("no questions")
	}
	seen := map[string]bool{}
	seenBird := map[int]bool{}
	for i, q := range qs {
		if err := ValidateBirdQuestion(q); err != nil {
			return fmt.Errorf("question %d (%s): %w", i, q.ID, err)
		}
		if seen[q.ID] {
			return fmt.Errorf("duplicate id %q", q.ID)
		}
		seen[q.ID] = true
		if seenBird[q.BirdID] {
			return fmt.Errorf("duplicate bird_id %d", q.BirdID)
		}
		seenBird[q.BirdID] = true
	}
	return nil
}

func ValidateBirdQuestion(q Question) error {
	if !birdIDPattern.MatchString(q.ID) {
		return fmt.Errorf("bad id %q, want bNN", q.ID)
	}
	switch q.Difficulty {
	case DifficultySimple, DifficultyModerate, DifficultyChallenging:
	default:
		return fmt.Errorf("bad difficulty %q, want simple, moderate or challenging", q.Difficulty)
	}
	if strings.TrimSpace(q.Text) == "" {
		return fmt.Errorf("empty question text")
	}
	if strings.TrimSpace(q.SQL) == "" {
		return fmt.Errorf("empty ground_truth_sql")
	}
	switch q.AnswerType {
	case TypeNumber, TypeString, TypeList, TypeRankedList, TypeTable, TypeFreeText:
	default:
		return fmt.Errorf("bad answer_type %q", q.AnswerType)
	}
	if tol := q.EffectiveTolerance(); tol < 0 || tol >= 1 {
		return fmt.Errorf("tolerance %v out of range [0, 1)", tol)
	}
	if strings.TrimSpace(q.Note) == "" {
		return fmt.Errorf("empty note")
	}
	if strings.TrimSpace(q.DbID) == "" {
		return fmt.Errorf("empty db_id")
	}
	if strings.TrimSpace(q.Evidence) == "" {
		return fmt.Errorf("empty evidence")
	}
	if q.BirdID <= 0 {
		return fmt.Errorf("bad bird_id %d", q.BirdID)
	}
	if err := db.Validate(q.SQL); err != nil {
		return err
	}
	return nil
}
