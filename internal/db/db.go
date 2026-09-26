package db

import (
	"context"
	"database/sql/driver"
	"encoding/csv"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Result struct {
	Columns   []string
	Rows      [][]string
	Truncated bool
}

var forbidden = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|TRUNCATE|GRANT|COPY|INTO)\b`)

var forbiddenFunc = regexp.MustCompile(`(?i)\b(set_config|pg_terminate_backend|pg_cancel_backend|pg_advisory_\w+|pg_try_advisory_\w+|pg_sleep\w*|pg_reload_conf|pg_read_file|pg_read_binary_file|pg_ls_dir|dblink\w*|lo_\w+)\s*\(`)

var dollarTag = regexp.MustCompile(`^\$[A-Za-z_]*\$`)

func MaskLiterals(stmt string) string {
	b := []byte(stmt)
	blank := func(from, to int) {
		for k := from; k < to && k < len(b); k++ {
			if b[k] != '\n' {
				b[k] = ' '
			}
		}
	}
	for i := 0; i < len(b); {
		switch {
		case b[i] == '-' && i+1 < len(b) && b[i+1] == '-':
			j := i
			for j < len(b) && b[j] != '\n' {
				j++
			}
			blank(i, j)
			i = j
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			j := strings.Index(string(b[i+2:]), "*/")
			end := len(b)
			if j >= 0 {
				end = i + 2 + j + 2
			}
			blank(i, end)
			i = end
		case b[i] == '\'' || b[i] == '"':
			q := b[i]
			j := i + 1
			for j < len(b) {
				if b[j] == q {
					if j+1 < len(b) && b[j+1] == q {
						j += 2
						continue
					}
					break
				}
				j++
			}
			blank(i+1, j)
			i = j + 1
		case b[i] == '$':
			tag := dollarTag.Find(b[i:])
			if tag == nil {
				i++
				continue
			}
			rest := string(b[i+len(tag):])
			j := strings.Index(rest, string(tag))
			end := len(b)
			if j >= 0 {
				end = i + len(tag) + j + len(tag)
			}
			blank(i+len(tag), end-len(tag))
			i = end
		default:
			i++
		}
	}
	return string(b)
}

func Validate(stmt string) error {
	trimmed := strings.TrimSpace(MaskLiterals(stmt))
	if trimmed == "" {
		return fmt.Errorf("empty statement")
	}
	if strings.Contains(trimmed, ";") {
		return fmt.Errorf("only a single statement without semicolons is allowed")
	}
	if found := forbidden.FindString(trimmed); found != "" {
		return fmt.Errorf("forbidden keyword: %s", strings.ToUpper(found))
	}
	if m := forbiddenFunc.FindStringSubmatch(trimmed); m != nil {
		return fmt.Errorf("forbidden function: %s", strings.ToLower(m[1]))
	}
	first := strings.ToUpper(strings.TrimLeft(trimmed, "( \t\n\r"))
	if i := strings.IndexAny(first, " \t\n\r("); i >= 0 {
		first = first[:i]
	}
	if first != "SELECT" && first != "WITH" {
		return fmt.Errorf("only SELECT or WITH statements are allowed")
	}
	return nil
}

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	return pgxpool.NewWithConfig(ctx, cfg)
}

func Query(ctx context.Context, pool *pgxpool.Pool, stmt string, maxRows int) (Result, error) {
	if maxRows <= 0 {
		return Result{}, fmt.Errorf("maxRows must be positive")
	}
	if err := Validate(stmt); err != nil {
		return Result{}, err
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '30s'"); err != nil {
		return Result{}, err
	}
	rows, err := tx.Query(ctx, stmt)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = f.Name
	}
	var out [][]string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return Result{}, err
		}
		row := make([]string, len(vals))
		for i, v := range vals {
			row[i] = stringify(v)
		}
		out = append(out, row)
		if len(out) > maxRows {
			break
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Result{}, err
	}
	rows.Close()
	result := Result{Columns: columns, Rows: out}
	if len(out) > maxRows {
		result.Rows = out[:maxRows]
		result.Truncated = true
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, err
	}
	return result, nil
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case [16]byte:
		return fmt.Sprintf("%x-%x-%x-%x-%x", t[0:4], t[4:6], t[6:8], t[8:10], t[10:16])
	case []any:
		parts := make([]string, len(t))
		for i, e := range t {
			parts[i] = stringify(e)
		}
		return "{" + strings.Join(parts, ",") + "}"
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	case driver.Valuer:
		dv, err := t.Value()
		if err != nil || dv == nil {
			return ""
		}
		return stringify(dv)
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (r Result) CSV() string {
	if len(r.Columns) == 0 && len(r.Rows) == 0 {
		return ""
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Write(r.Columns)
	for _, row := range r.Rows {
		w.Write(row)
	}
	w.Flush()
	return b.String()
}
