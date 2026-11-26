package db

import (
	"context"
	"database/sql"
	"errors"
)

// Database owns the business pool and its independent liveness connection.
// Repositories execute native MySQL through database/sql directly.
type Database struct {
	*sql.DB
	healthPool     *sql.DB
	statisticsPool *sql.DB
}

func (d *Database) Close() error {
	var statisticsErr error
	if d.statisticsPool != nil {
		statisticsErr = d.statisticsPool.Close()
	}
	return errors.Join(statisticsErr, d.healthPool.Close(), d.DB.Close())
}

// StatisticsPool uses the same primary MySQL server, with an independent
// connection budget. It never bypasses authorization or transaction locks.
func (d *Database) StatisticsPool() *sql.DB {
	if d.statisticsPool != nil {
		return d.statisticsPool
	}
	return d.DB
}

func (d *Database) PingContext(ctx context.Context) error {
	return d.healthPool.PingContext(ctx)
}

type statementExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// InsertID obtains the key from the INSERT result on the executing connection.
func InsertID(ctx context.Context, store statementExecutor, query string, args ...any) (int64, error) {
	result, err := store.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
