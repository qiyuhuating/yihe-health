// Package vitalrepo owns persisted vital alerts and their resolution.
package vitalrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/healthalertstate"
)

var ErrAlertNotFound = errors.New("vital alert record not found")

const pendingRate = "(r.rate_state='1' AND (r.rate_handled_at IS NULL OR r.create_time > r.rate_handled_at))"
const pendingPressure = "(r.pre_state='1' AND (r.pre_handled_at IS NULL OR r.create_time > r.pre_handled_at))"

// Both counters evaluate the same history rows, including rows pending both metrics.
const pendingCountColumns = `COUNT(CASE WHEN ` + pendingRate + ` THEN 1 END) AS rate_count,
	COUNT(CASE WHEN ` + pendingPressure + ` THEN 1 END) AS pressure_count`

const pendingCountsSQL = `SELECT ` + pendingCountColumns + ` FROM ratepre r`

func AlertCounts(ctx context.Context, store db.RowQuerier) (rate, pressure int64, err error) {
	err = store.QueryRowContext(ctx, pendingCountsSQL).Scan(&rate, &pressure)
	return
}

func updateSQL(kind string) string {
	if kind == "pre" {
		return "UPDATE ratepre AS r SET pre_handled=1, pre_handled_at=CURRENT_TIMESTAMP(6), pre_state='0'"
	}
	return "UPDATE ratepre AS r SET rate_handled=1, rate_handled_at=CURRENT_TIMESTAMP(6), rate_state='0'"
}

// ResolveAlert updates one or both metrics within the caller's authorization transaction.
func ResolveAlert(ctx context.Context, tx *sql.Tx, recordID int64, kind string) (bool, error) {
	var deviceID *int64
	err := tx.QueryRowContext(ctx, "SELECT device_id FROM ratepre WHERE id=?", recordID).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrAlertNotFound
	}
	if err != nil {
		return false, err
	}
	kinds := []string{"rate", "pre"}
	if kind == "rate" || kind == "pre" {
		kinds = []string{kind}
	}
	var affected int64
	for _, metric := range kinds {
		pending := pendingRate
		if metric == "pre" {
			pending = pendingPressure
		}
		result, err := tx.ExecContext(ctx, updateSQL(metric)+" WHERE id=? AND "+pending, recordID)
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		affected += changed
	}
	if affected == 0 {
		return false, nil
	}
	if err := healthalertstate.SyncVitalsIfPresent(ctx, tx, deviceID); err != nil {
		return false, err
	}
	return true, nil
}

// ResolveDeviceAlert updates the selected metric and the device-level handled flag
// within the caller's transaction. The caller owns locking and commit.
func ResolveDeviceAlert(ctx context.Context, tx *sql.Tx, deviceID int64, kind string, recordID *int64) (int64, error) {
	handled, pending := "rate_handled", pendingRate
	if kind == "pre" {
		handled, pending = "pre_handled", pendingPressure
	}
	query := updateSQL(kind) + " WHERE device_id=? AND "
	args := []any{deviceID}
	if recordID != nil {
		query += "id=? AND "
		args = append(args, *recordID)
	}
	query += pending
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return changed, err
	}
	heartPending, pressurePending, err := healthalertstate.SyncVitals(ctx, tx, deviceID)
	if err != nil {
		return 0, err
	}
	hasPending := heartPending
	if kind == "pre" {
		hasPending = pressurePending
	}
	if !hasPending {
		if _, err := tx.ExecContext(ctx, "UPDATE device SET "+handled+"=1 WHERE device_id=?", deviceID); err != nil {
			return 0, err
		}
	}
	return changed, nil
}
