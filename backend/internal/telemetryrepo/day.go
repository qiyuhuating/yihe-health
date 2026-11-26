package telemetryrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/healthalertstate"
)

const watchDayUpsertSQL = `INSERT INTO watch_day_info(device_id, sleep, step, sleep_sate, create_time, step_observed_at, old_id, binding_epoch, sleep_observed_at, sleep_state_observed_at)
 VALUES(?,?,?,?,?,?,?,?,?,?) AS new
 ON DUPLICATE KEY UPDATE
	sleep = CASE WHEN new.sleep_observed_at IS NOT NULL AND (watch_day_info.sleep_observed_at IS NULL OR new.sleep_observed_at>=watch_day_info.sleep_observed_at) THEN new.sleep ELSE watch_day_info.sleep END,
	step = CASE WHEN new.step_observed_at IS NOT NULL AND (watch_day_info.step_observed_at IS NULL OR new.step_observed_at>=watch_day_info.step_observed_at) THEN new.step ELSE watch_day_info.step END,
	step_observed_at = COALESCE(GREATEST(watch_day_info.step_observed_at,new.step_observed_at),watch_day_info.step_observed_at,new.step_observed_at),
	sleep_sate = CASE WHEN new.sleep_state_observed_at IS NOT NULL AND (watch_day_info.sleep_state_observed_at IS NULL OR new.sleep_state_observed_at>=watch_day_info.sleep_state_observed_at) THEN new.sleep_sate ELSE watch_day_info.sleep_sate END,
	sleep_observed_at = COALESCE(GREATEST(watch_day_info.sleep_observed_at,new.sleep_observed_at),watch_day_info.sleep_observed_at,new.sleep_observed_at),
	sleep_state_observed_at = COALESCE(GREATEST(watch_day_info.sleep_state_observed_at,new.sleep_state_observed_at),watch_day_info.sleep_state_observed_at,new.sleep_state_observed_at)`

// PersistDay updates a bound device's daily sample inside the caller's transaction.
func PersistDay(ctx context.Context, tx *sql.Tx, deviceID, oldID, healthEpoch int64, day, beTime any, step, sleep *int, sleepState *string) error {
	if step != nil || sleep != nil || sleepState != nil {
		var rowID, epoch int64
		var owner *int64
		err := tx.QueryRowContext(ctx, `SELECT id,old_id,binding_epoch FROM watch_day_info
		 WHERE device_id=? AND create_time=? FOR UPDATE`, deviceID, day).Scan(&rowID, &owner, &epoch)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (owner == nil || *owner != oldID || epoch != healthEpoch) {
			// Preserve the previous binding's snapshot before starting an empty one.
			if _, err = tx.ExecContext(ctx, `INSERT INTO watch_day_info_archive
			 (device_id,sleep,step,sleep_sate,create_time,step_observed_at,old_id,binding_epoch,sleep_observed_at,sleep_state_observed_at)
			 SELECT device_id,sleep,step,sleep_sate,create_time,step_observed_at,old_id,binding_epoch,sleep_observed_at,sleep_state_observed_at
			 FROM watch_day_info WHERE id=?`, rowID); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE watch_day_info SET sleep=NULL,step=NULL,sleep_sate=NULL,
			 step_observed_at=NULL,sleep_observed_at=NULL,sleep_state_observed_at=NULL,old_id=?,binding_epoch=? WHERE id=?`, oldID, healthEpoch, rowID); err != nil {
				return err
			}
		}
		var stepAny, sleepAny, stateAny, stepObservedAt, sleepObservedAt, sleepStateObservedAt any
		if step != nil {
			stepAny = *step
			stepObservedAt = beTime
		}
		if sleep != nil {
			sleepAny = *sleep
			sleepObservedAt = beTime
		}
		if sleepState != nil {
			stateAny = *sleepState
			sleepStateObservedAt = beTime
		}
		if _, err := tx.ExecContext(ctx, watchDayUpsertSQL,
			deviceID, sleepAny, stepAny, stateAny, day, stepObservedAt, oldID, healthEpoch, sleepObservedAt, sleepStateObservedAt); err != nil {
			return err
		}
		if err := healthalertstate.RecordSleep(ctx, tx, deviceID, healthEpoch, sleepStateObservedAt, sleepState); err != nil {
			return err
		}
	}
	return nil
}
