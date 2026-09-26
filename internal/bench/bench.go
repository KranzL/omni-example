package bench

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/KranzL/omni-example/internal/db"
	"gopkg.in/yaml.v3"
)

const (
	DifficultyEasy     = "easy"
	DifficultyModerate = "moderate"
	DifficultyHard     = "hard"
	DifficultyExpert   = "expert"

	DifficultySimple      = "simple"
	DifficultyChallenging = "challenging"

	TypeNumber     = "number"
	TypeString     = "string"
	TypeList       = "list"
	TypeRankedList = "ranked_list"
	TypeTable      = "table"
	TypeFreeText   = "free_text"

	DefaultNumberTolerance = 0.01

	BenchDir       = "bench"
	QuestionsFile  = "questions.yaml"
	AnswersFile    = "answers.json"
	RouterSeedFile = "router_seed.yaml"
	MaxAnswerRows  = 5000

	BenchEcomm = "ecomm"
	BenchBird  = "bird"

	BirdDirName       = "bird"
	BirdQuestionsFile = "questions.yaml"
	BirdAnswersFile   = "answers.json"
	BirdSeedFile      = "router_seed.yaml"
	BirdDBDir         = "db"
	BirdSemanticDir   = "semantic"
)

var idPattern = regexp.MustCompile(`^([emhx])(\d{2})$`)

type Question struct {
	ID         string   `yaml:"id"`
	Difficulty string   `yaml:"difficulty"`
	Text       string   `yaml:"question"`
	SQL        string   `yaml:"ground_truth_sql"`
	AnswerType string   `yaml:"answer_type"`
	Tolerance  *float64 `yaml:"tolerance"`
	Note       string   `yaml:"note"`
	DbID       string   `yaml:"db_id,omitempty"`
	Evidence   string   `yaml:"evidence,omitempty"`
	BirdID     int      `yaml:"bird_id,omitempty"`
}

func (q Question) EffectiveTolerance() float64 {
	if q.Tolerance != nil {
		return *q.Tolerance
	}
	if q.AnswerType == TypeNumber {
		return DefaultNumberTolerance
	}
	return 0
}

type TableAnswer struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

type SnapshotEntry struct {
	ID         string `json:"id"`
	Answer     any    `json:"answer"`
	ComputedAt string `json:"computed_at"`
}

func Load(path string) ([]Question, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var qs []Question
	if err := yaml.Unmarshal(data, &qs); err != nil {
		return nil, err
	}
	if err := Validate(qs); err != nil {
		return nil, err
	}
	return qs, nil
}

func Validate(qs []Question) error {
	if len(qs) == 0 {
		return fmt.Errorf("no questions")
	}
	seen := map[string]bool{}
	for i, q := range qs {
		if err := ValidateQuestion(q); err != nil {
			return fmt.Errorf("question %d (%s): %w", i, q.ID, err)
		}
		if seen[q.ID] {
			return fmt.Errorf("duplicate id %q", q.ID)
		}
		seen[q.ID] = true
	}
	return nil
}

func ValidateQuestion(q Question) error {
	m := idPattern.FindStringSubmatch(q.ID)
	if m == nil {
		return fmt.Errorf("bad id %q, want eNN, mNN, hNN or xNN", q.ID)
	}
	wantDifficulty := map[string]string{
		"e": DifficultyEasy,
		"m": DifficultyModerate,
		"h": DifficultyHard,
		"x": DifficultyExpert,
	}[m[1]]
	if q.Difficulty != wantDifficulty {
		return fmt.Errorf("id %q requires difficulty %q, got %q", q.ID, wantDifficulty, q.Difficulty)
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
	if err := db.Validate(q.SQL); err != nil {
		return err
	}
	return nil
}

func BuildAnswer(q Question, res db.Result) (any, error) {
	switch q.AnswerType {
	case TypeNumber:
		if len(res.Rows) != 1 || len(res.Columns) != 1 {
			return nil, fmt.Errorf("number answer needs 1 row and 1 column, got %d rows and %d columns", len(res.Rows), len(res.Columns))
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(res.Rows[0][0]), 64)
		if err != nil {
			return nil, fmt.Errorf("number answer is not numeric: %q", res.Rows[0][0])
		}
		return v, nil
	case TypeString, TypeFreeText:
		if len(res.Rows) != 1 || len(res.Columns) != 1 {
			return nil, fmt.Errorf("string answer needs 1 row and 1 column, got %d rows and %d columns", len(res.Rows), len(res.Columns))
		}
		return res.Rows[0][0], nil
	case TypeList, TypeRankedList:
		if len(res.Columns) != 1 {
			return nil, fmt.Errorf("list answer needs 1 column, got %d", len(res.Columns))
		}
		if len(res.Rows) == 0 {
			return nil, fmt.Errorf("list answer has no rows")
		}
		out := make([]string, len(res.Rows))
		for i, row := range res.Rows {
			out[i] = row[0]
		}
		return out, nil
	case TypeTable:
		if len(res.Columns) == 0 || len(res.Rows) == 0 {
			return nil, fmt.Errorf("table answer needs at least 1 column and 1 row")
		}
		rows := make([][]string, len(res.Rows))
		for i, row := range res.Rows {
			cp := make([]string, len(row))
			copy(cp, row)
			rows[i] = cp
		}
		return TableAnswer{Columns: res.Columns, Rows: rows}, nil
	default:
		return nil, fmt.Errorf("bad answer_type %q", q.AnswerType)
	}
}

func FormatAnswer(answer any) string {
	switch v := answer.(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64) + "\n"
	case string:
		return v + "\n"
	case []string:
		return strings.Join(v, "\n") + "\n"
	case TableAnswer:
		r := db.Result{Columns: v.Columns, Rows: v.Rows}
		return r.CSV()
	default:
		return fmt.Sprintf("%v\n", v)
	}
}

func FindBenchDir(startDir string) string {
	dir := startDir
	for i := 0; i < 12; i++ {
		if info, err := os.Stat(filepath.Join(dir, BenchDir, QuestionsFile)); err == nil && !info.IsDir() {
			return filepath.Join(dir, BenchDir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}
