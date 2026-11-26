package telemetryrepo

import (
	"context"
	"database/sql"
	"time"
)

const watchProfileInsertSQL = `INSERT INTO watch_info(device_id, op_time, bpf, bpl, hrf, hrl) VALUES(?,?,?,?,?,?)
	 ON DUPLICATE KEY UPDATE device_id=device_id`

// EnsureDefaultWatchProfile creates thresholds for a new device without replacing customized values.
func EnsureDefaultWatchProfile(ctx context.Context, tx *sql.Tx, deviceID int64, day time.Time) error {
	_, err := tx.ExecContext(ctx, watchProfileInsertSQL, deviceID, day, 140, 90, 110, 55)
	return err
}
