package healthalertstate

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const upsertVitalsSQL = `INSERT INTO device_health_alert_state(
	device_id,binding_epoch,heart_rate_alert,blood_pressure_alert,sleep_alert,vitals_observed_at
) VALUES(?,?,?,?,false,?) AS new
ON DUPLICATE KEY UPDATE
	heart_rate_alert=CASE
		WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.heart_rate_alert
		ELSE device_health_alert_state.heart_rate_alert OR new.heart_rate_alert
	END,
	blood_pressure_alert=CASE
		WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.blood_pressure_alert
		ELSE device_health_alert_state.blood_pressure_alert OR new.blood_pressure_alert
	END,
	sleep_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN false ELSE device_health_alert_state.sleep_alert END,
	vitals_observed_at=CASE
		WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.vitals_observed_at
		ELSE COALESCE(GREATEST(device_health_alert_state.vitals_observed_at,new.vitals_observed_at),device_health_alert_state.vitals_observed_at,new.vitals_observed_at)
	END,
	sleep_observed_at=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN NULL ELSE device_health_alert_state.sleep_observed_at END,
	binding_epoch=new.binding_epoch`

const setVitalsSQL = `INSERT INTO device_health_alert_state(
	device_id,binding_epoch,heart_rate_alert,blood_pressure_alert,sleep_alert,vitals_observed_at
) VALUES(?,?,?,?,false,CURRENT_TIMESTAMP(6)) AS new
ON DUPLICATE KEY UPDATE
	heart_rate_alert=new.heart_rate_alert,
	blood_pressure_alert=new.blood_pressure_alert,
	sleep_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN false ELSE device_health_alert_state.sleep_alert END,
	vitals_observed_at=new.vitals_observed_at,
	sleep_observed_at=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN NULL ELSE device_health_alert_state.sleep_observed_at END,
	binding_epoch=new.binding_epoch`

const upsertSleepSQL = `INSERT INTO device_health_alert_state(
	device_id,binding_epoch,heart_rate_alert,blood_pressure_alert,sleep_alert,sleep_observed_at
) VALUES(?,?,false,false,?,?) AS new
ON DUPLICATE KEY UPDATE
	heart_rate_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN false ELSE device_health_alert_state.heart_rate_alert END,
	blood_pressure_alert=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN false ELSE device_health_alert_state.blood_pressure_alert END,
	sleep_alert=CASE
		WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.sleep_alert
		WHEN new.sleep_observed_at IS NOT NULL AND (device_health_alert_state.sleep_observed_at IS NULL OR new.sleep_observed_at>=device_health_alert_state.sleep_observed_at) THEN new.sleep_alert
		ELSE device_health_alert_state.sleep_alert
	END,
	vitals_observed_at=CASE WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN NULL ELSE device_health_alert_state.vitals_observed_at END,
	sleep_observed_at=CASE
		WHEN device_health_alert_state.binding_epoch<>new.binding_epoch THEN new.sleep_observed_at
		ELSE COALESCE(GREATEST(device_health_alert_state.sleep_observed_at,new.sleep_observed_at),device_health_alert_state.sleep_observed_at,new.sleep_observed_at)
	END,
	binding_epoch=new.binding_epoch`

func active(state any) bool {
	value, ok := state.(string)
	return ok && value == "1"
}

// RecordVitals folds newly persisted vital alerts into the current binding snapshot.
// Normal samples do not clear an older unresolved alert; resolution owns that transition.
func RecordVitals(ctx context.Context, tx *sql.Tx, deviceID, epoch int64, observedAt time.Time, heartState, pressureState any) error {
	_, err := tx.ExecContext(ctx, upsertVitalsSQL,
		deviceID, epoch, active(heartState), active(pressureState), observedAt)
	return err
}

// RecordSleep is latest-observation-wins because sleep_sate represents current daily classification.
func RecordSleep(ctx context.Context, tx *sql.Tx, deviceID, epoch int64, observedAt any, state *string) error {
	if state == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, upsertSleepSQL, deviceID, epoch, *state == "1", observedAt)
	return err
}

// SyncVitals recomputes the pending vital flags after a human resolution.
// Current binding ownership and bind time prevent earlier residents from leaking into the snapshot.
func SyncVitals(ctx context.Context, tx *sql.Tx, deviceID int64) (heart, pressure bool, err error) {
	var epoch int64
	err = tx.QueryRowContext(ctx, `SELECT d.health_epoch,
		EXISTS(SELECT 1 FROM ratepre r WHERE r.device_id=d.device_id AND r.old_id=d.old_id
			AND r.rate_state='1' AND (d.bind_time IS NULL OR r.create_time>=d.bind_time)),
		EXISTS(SELECT 1 FROM ratepre r WHERE r.device_id=d.device_id AND r.old_id=d.old_id
			AND r.pre_state='1' AND (d.bind_time IS NULL OR r.create_time>=d.bind_time))
		FROM device d WHERE d.device_id=?`, deviceID).Scan(&epoch, &heart, &pressure)
	if err != nil {
		return false, false, err
	}
	_, err = tx.ExecContext(ctx, setVitalsSQL, deviceID, epoch, heart, pressure)
	return heart, pressure, err
}

func SyncVitalsIfPresent(ctx context.Context, tx *sql.Tx, deviceID *int64) error {
	if deviceID == nil {
		return nil
	}
	_, _, err := SyncVitals(ctx, tx, *deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}
