package care

import (
	"context"
	"database/sql"
	"strings"
	"time"

	dbstore "yihe-care/internal/db"
)

// scanOffline is triggered every 30 seconds. The due index selects only
// expired online sessions, including unbound devices. Lock actual device
// rows by primary key, never by the mutable secondary due index.
func scanOffline(ctx context.Context, pool *dbstore.Database) error {
	var cutoff time.Time
	if err := pool.QueryRowContext(ctx, "SELECT CURRENT_TIMESTAMP(6)").Scan(&cutoff); err != nil {
		return err
	}
	var after time.Time
	var afterID int64
	for {
		query := "SELECT device_id,offline_due_at FROM device FORCE INDEX(idx_device_offline_due) WHERE offline_due_at<?"
		args := []any{cutoff}
		if !after.IsZero() {
			query += " AND (offline_due_at>? OR (offline_due_at=? AND device_id>?))"
			args = append(args, after, after, afterID)
		}
		query += " ORDER BY offline_due_at,device_id LIMIT 100"
		rows, err := pool.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		ids := make([]any, 0, 100)
		for rows.Next() {
			if err = rows.Scan(&afterID, &after); err != nil {
				break
			}
			ids = append(ids, afterID)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err = resetOfflinePage(ctx, pool, cutoff, ids); err != nil {
			return err
		}
		if len(ids) < 100 {
			return nil
		}
	}
}

func resetOfflinePage(ctx context.Context, pool *dbstore.Database, cutoff time.Time, ids []any) error {
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	// Revalidate after taking the primary record lock. A heartbeat or
	// another worker may have changed the deadline since candidate selection.
	rows, err := tx.QueryContext(ctx, "SELECT device_id FROM device FORCE INDEX(PRIMARY) WHERE device_id IN ("+placeholders+") AND offline_due_at<? ORDER BY device_id FOR UPDATE SKIP LOCKED", append(ids, cutoff)...)
	if err != nil {
		return err
	}
	locked := make([]any, 0, len(ids))
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			break
		}
		locked = append(locked, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if len(locked) == 0 {
		return nil
	}
	placeholders = strings.TrimSuffix(strings.Repeat("?,", len(locked)), ",")
	_, err = tx.ExecContext(ctx, "UPDATE device SET boot_seq=boot_seq+1,person_handled=0,last_online_at=NULL WHERE device_id IN ("+placeholders+")", locked...)
	if err != nil {
		return err
	}
	return tx.Commit()
}
