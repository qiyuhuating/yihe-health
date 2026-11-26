package devicerepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/telemetryrepo"
)

var ErrResidentNotFound = errors.New("resident not found")
var ErrDeviceInsertFailed = errors.New("device insert failed")

type NewDevice struct {
	Number     string
	SecretHash string
	Alias      string
	Model      string
	ResidentID *int64
	At         time.Time
	ProfileDay time.Time
}

// Create registers a device and its optional resident/profile association atomically.
func Create(ctx context.Context, tx *sql.Tx, input NewDevice) (int64, error) {

	var residentValue, bindTime any
	if input.ResidentID != nil {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oldperson WHERE old_id=?)`, *input.ResidentID).Scan(&exists); err != nil {
			return 0, err
		}
		if !exists {
			return 0, ErrResidentNotFound
		}
		residentValue = *input.ResidentID
		bindTime = input.At
	}
	deviceID, err := db.InsertID(ctx, tx,
		`INSERT INTO device(device_no, secret, alias, model, old_id, status, bind_time, create_time)
		 VALUES(?,?,?,?,?,1,?,?)`,
		input.Number, input.SecretHash, input.Alias, input.Model,
		residentValue, bindTime, input.At)
	if err != nil {
		return 0, err
	}
	if deviceID == 0 {
		return 0, ErrDeviceInsertFailed
	}
	if input.ResidentID != nil {
		result, err := tx.ExecContext(ctx, `UPDATE oldperson SET device_id=COALESCE(device_id,?) WHERE old_id=?`, deviceID, *input.ResidentID)
		if err != nil {
			return 0, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if affected != 1 {
			return 0, ErrResidentNotFound
		}
		if err := telemetryrepo.EnsureDefaultWatchProfile(ctx, tx, deviceID, input.ProfileDay); err != nil {
			return 0, err
		}
	}
	return deviceID, nil
}
