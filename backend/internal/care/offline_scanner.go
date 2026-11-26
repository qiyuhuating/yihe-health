package care

import (
	"context"
	"database/sql"
	"strings"
	dbstore "yihe-care/internal/db"
)

// scanOffline is triggered every 30 seconds after the worker starts. It shares
// the lifecycle and cancellation of the database worker and does not survive a
// server shutdown or capture a stale Handler/Pool instance.
func scanOffline(ctx context.Context, pool *dbstore.Database) error {
	var after int64
	for {
		last, count, err := scanOfflinePage(ctx, pool, after)
		if err != nil || count == 0 {
			return err
		}
		after = last
	}
}

func scanOfflinePage(ctx context.Context, pool *dbstore.Database, after int64) (int64, int, error) {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return after, 0, err
	}
	defer tx.Rollback()
	// Skip busy devices instead of holding a table-wide UPDATE's earlier locks
	// while waiting for an ingest/rule transaction. Retry skipped rows next tick.
	rows, err := tx.QueryContext(ctx, `SELECT device_id FROM device
		WHERE device_id>? AND status=1 AND last_online_at IS NOT NULL AND last_time IS NOT NULL
		AND last_online_at >= last_time
		AND last_time < CURRENT_TIMESTAMP(6) - INTERVAL 120 SECOND
		ORDER BY device_id LIMIT 100 FOR UPDATE SKIP LOCKED`, after)
	if err != nil {
		return after, 0, err
	}
	args := make([]any, 0, 100)
	placeholders := make([]string, 0, 100)
	for rows.Next() {
		if err = rows.Scan(&after); err != nil {
			break
		}
		args = append(args, after)
		placeholders = append(placeholders, "?")
	}
	if err == nil {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return after, 0, err
	}
	if len(args) != 0 {
		_, err = tx.ExecContext(ctx, `UPDATE device SET boot_seq=boot_seq+1,person_handled=0,last_online_at=NULL
		WHERE device_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return after, 0, err
		}
	}
	return after, len(args), tx.Commit()
}
