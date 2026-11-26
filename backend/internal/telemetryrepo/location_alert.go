package telemetryrepo

import (
	"context"
	"database/sql"

	"yihe-care/internal/db"
	"yihe-care/internal/locationalertstate"
)

// ResolveLatestLocationAlert marks the newest location alarm acknowledged.
// The query ordering matches the administrative alert-detail endpoint.
func ResolveLatestLocationAlert(ctx context.Context, store *db.Database, residentID int64) (bool, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	resolved, err := ResolveLatestLocationAlertTx(ctx, tx, residentID)
	if err != nil || !resolved {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveLatestLocationAlertTx shares the caller's authorization transaction.
// Duty revocation cannot overtake the location-alert write.
func ResolveLatestLocationAlertTx(ctx context.Context, tx *sql.Tx, residentID int64) (bool, error) {
	alarmID, found, err := locationalertstate.LockActive(ctx, tx, residentID)
	if err != nil || !found {
		return false, err
	}
	if err := locationalertstate.Resolve(ctx, tx, residentID, alarmID); err != nil {
		return false, err
	}
	return true, nil
}
