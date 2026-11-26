package db

import (
	"context"
	"database/sql"
)

// RowQuerier is the read-only query capability shared by databases and transactions.
type RowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// RowsQuerier supports scanning rows through either a database or a transaction.
type RowsQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// ReadQuerier supports both row and collection reads inside a database or transaction.
type ReadQuerier interface {
	RowQuerier
	RowsQuerier
}

// QueryEach visits rows in query order and closes the result before returning.
func QueryEach(ctx context.Context, q RowsQuerier, visit func(*sql.Rows) error, query string, args ...any) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := visit(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// QueryList scans all rows and closes the result before returning to the caller.
func QueryList[T any](ctx context.Context, q RowsQuerier, scan func(*sql.Rows) (T, error), query string, args ...any) ([]T, error) {
	items := []T{}
	err := QueryEach(ctx, q, func(rows *sql.Rows) error {
		item, err := scan(rows)
		if err == nil {
			items = append(items, item)
		}
		return err
	}, query, args...)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func ScanValue[T any](row *sql.Rows) (T, error) {
	var value T
	err := row.Scan(&value)
	return value, err
}
