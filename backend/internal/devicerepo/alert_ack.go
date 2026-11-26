package devicerepo

import (
	"context"
	"database/sql"
)

// ExistsInTransaction checks inventory under the caller's alert transaction.
func ExistsInTransaction(ctx context.Context, tx *sql.Tx, deviceID int64) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM device WHERE device_id=?)", deviceID).Scan(&exists)
	return exists, err
}

// AcknowledgePersonAlert remembers the boot sequence seen by the operator.
// Reconnects that increment boot_seq can create another offline alert.
func AcknowledgePersonAlert(ctx context.Context, tx *sql.Tx, deviceID int64) (int64, error) {
	result, err := tx.ExecContext(ctx,
		`UPDATE device SET person_handled=1, person_handled_at=CURRENT_TIMESTAMP(6), handled_seq=boot_seq
		 WHERE device_id=?`, deviceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
