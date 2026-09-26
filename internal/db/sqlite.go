package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite"
)

type Backend interface {
	Query(ctx context.Context, stmt string, maxRows int) (Result, error)
}

type PGBackend struct {
	Pool *pgxpool.Pool
}

func (b PGBackend) Query(ctx context.Context, stmt string, maxRows int) (Result, error) {
	return Query(ctx, b.Pool, stmt, maxRows)
}

func OpenSQLite(path string) (*sql.DB, error) {
	dsn := url.URL{Scheme: "file", Opaque: path, RawQuery: "mode=ro&_pragma=query_only(1)"}
	sqldb, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	return sqldb, nil
}

type SQLiteBackend struct {
	DB *sql.DB
}

func (b SQLiteBackend) Query(ctx context.Context, stmt string, maxRows int) (Result, error) {
	if maxRows <= 0 {
		return Result{}, fmt.Errorf("maxRows must be positive")
	}
	if err := Validate(stmt); err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows, err := b.DB.QueryContext(ctx, stmt)
	if err != nil {
		return Result{}, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return Result{}, err
	}
	vals := make([]any, len(columns))
	ptrs := make([]any, len(columns))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	var out [][]string
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
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
		return Result{}, err
	}
	result := Result{Columns: columns, Rows: out}
	if len(out) > maxRows {
		result.Rows = out[:maxRows]
		result.Truncated = true
	}
	return result, nil
}

var _ Backend = PGBackend{}
var _ Backend = SQLiteBackend{}
