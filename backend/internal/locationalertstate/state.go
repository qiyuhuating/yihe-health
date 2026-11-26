package locationalertstate

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const upsertSQL = `INSERT INTO resident_location_alert_state(old_id,alarm_id,active,observed_at)
 VALUES(?,?,true,?) AS new
 ON DUPLICATE KEY UPDATE
	active=CASE
		WHEN resident_location_alert_state.observed_at IS NULL OR new.observed_at>resident_location_alert_state.observed_at OR (new.observed_at=resident_location_alert_state.observed_at AND new.alarm_id>resident_location_alert_state.alarm_id) THEN true
		ELSE resident_location_alert_state.active
	END,
	alarm_id=CASE
		WHEN resident_location_alert_state.observed_at IS NULL OR new.observed_at>resident_location_alert_state.observed_at OR (new.observed_at=resident_location_alert_state.observed_at AND new.alarm_id>resident_location_alert_state.alarm_id) THEN new.alarm_id
		ELSE resident_location_alert_state.alarm_id
	END,
	observed_at=CASE
		WHEN resident_location_alert_state.observed_at IS NULL OR new.observed_at>resident_location_alert_state.observed_at OR (new.observed_at=resident_location_alert_state.observed_at AND new.alarm_id>=resident_location_alert_state.alarm_id) THEN new.observed_at
		ELSE resident_location_alert_state.observed_at
	END`

func Record(ctx context.Context, tx *sql.Tx, residentID, alarmID int64, observedAt time.Time) error {
	_, err := tx.ExecContext(ctx, upsertSQL, residentID, alarmID, observedAt)
	return err
}

func LockActive(ctx context.Context, tx *sql.Tx, residentID int64) (int64, bool, error) {
	var alarmID int64
	err := tx.QueryRowContext(ctx,
		"SELECT alarm_id FROM resident_location_alert_state WHERE old_id=? AND active=true FOR UPDATE",
		residentID).Scan(&alarmID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return alarmID, err == nil, err
}

// Resolve locks the resident snapshot before changing its alarm, sharing one
// lock order across direct resolution and care verification. Resolving an older
// alarm leaves a newer snapshot active.
func Resolve(ctx context.Context, tx *sql.Tx, residentID, alarmID int64) error {
	var currentAlarm int64
	if err := tx.QueryRowContext(ctx,
		"SELECT alarm_id FROM resident_location_alert_state WHERE old_id=? FOR UPDATE", residentID).Scan(&currentAlarm); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE alarm_precess a
		LEFT JOIN resident_location_alert_state s ON s.old_id=a.old_id AND s.alarm_id=a.id
		SET a.status='0',s.active=false WHERE a.old_id=? AND a.id=?`, residentID, alarmID)
	return err
}
