package bench

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/KranzL/omni-example/internal/db"
)

var orderByKeyword = regexp.MustCompile(`(?i)\border\s+by\b`)

func HasOrderBy(stmt string) bool {
	masked := []byte(db.MaskLiterals(stmt))
	depth := 0
	for i, c := range masked {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		default:
			if depth > 0 {
				masked[i] = ' '
			}
		}
	}
	return orderByKeyword.Match(masked)
}

func ExecMatch(gold, pred db.Result, ordered bool) bool {
	if len(gold.Columns) != len(pred.Columns) {
		return false
	}
	if len(gold.Rows) != len(pred.Rows) {
		return false
	}
	if ordered {
		for i := range gold.Rows {
			if execRowKey(gold.Rows[i]) != execRowKey(pred.Rows[i]) {
				return false
			}
		}
		return true
	}
	counts := make(map[string]int, len(gold.Rows))
	for _, row := range gold.Rows {
		counts[execRowKey(row)]++
	}
	for _, row := range pred.Rows {
		key := execRowKey(row)
		if counts[key] == 0 {
			return false
		}
		counts[key]--
	}
	return true
}

func execRowKey(row []string) string {
	data, err := json.Marshal(row)
	if err != nil {
		return strings.Join(row, "\x00")
	}
	return string(data)
}

type EXGrader interface {
	ExecCorrect(ctx context.Context, q Question, modelSQL string) (bool, error)
}

type BirdEX struct {
	DBs map[string]*sql.DB
}

func (b BirdEX) ExecCorrect(ctx context.Context, q Question, modelSQL string) (bool, error) {
	sqldb, ok := b.DBs[q.DbID]
	if !ok {
		return false, fmt.Errorf("unknown db_id %q", q.DbID)
	}
	model := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(modelSQL), ";"))
	if model == "" {
		return false, nil
	}
	backend := db.SQLiteBackend{DB: sqldb}
	gold, err := backend.Query(ctx, q.SQL, MaxAnswerRows)
	if err != nil {
		return false, err
	}
	pred, err := backend.Query(ctx, model, MaxAnswerRows)
	if err != nil {
		return false, nil
	}
	return ExecMatch(gold, pred, HasOrderBy(q.SQL)), nil
}
