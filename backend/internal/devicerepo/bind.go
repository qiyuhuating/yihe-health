package devicerepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/telemetryrepo"
)

var ErrBindFailed = errors.New("device binding failed")
var ErrBindCodeUsed = errors.New("binding code already consumed")

type Binding struct {
	DeviceID         int64
	ResidentID       int64
	FamilyID         *int64
	CodeID           int64
	ExistingDeviceID *int64
	ExistingFamilyID *int64
	At               time.Time
	ProfileDay       time.Time
}

// LockResident ensures binding-related operations acquire the resident lock before the device lock.
func LockResident(ctx context.Context, tx *sql.Tx, residentID int64) error {
	var id int64
	return tx.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE", residentID).Scan(&id)
}

// LockDeviceID holds the device row lock for the caller's transaction.
func LockDeviceID(ctx context.Context, tx *sql.Tx, deviceID int64) error {
	var id int64
	return tx.QueryRowContext(ctx, "SELECT device_id FROM device WHERE device_id=? FOR UPDATE", deviceID).Scan(&id)
}

// LockedDeviceRow selects the current credential and binding state under a row lock.
// The caller locks the resident first and retains the transaction until binding completes.
func LockedDeviceRow(ctx context.Context, tx *sql.Tx, deviceID int64) *sql.Row {
	return tx.QueryRowContext(ctx, `SELECT `+deviceRecordColumns+` FROM device WHERE device_id=? FOR UPDATE`, deviceID)
}

// CompleteBinding updates only the locked device and its resident references.
// The caller owns the transaction and must commit it after all validations succeed.
func CompleteBinding(ctx context.Context, tx *sql.Tx, binding Binding) error {
	result, err := tx.ExecContext(ctx,
		`UPDATE device SET old_id=?, family_id=?, bind_time=?,
		 last_time=GREATEST(COALESCE(last_time,?),?) WHERE device_id=?`,
		binding.ResidentID, binding.FamilyID, binding.At, binding.At, binding.At, binding.DeviceID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrBindFailed
	}
	if binding.ExistingDeviceID == nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE oldperson SET device_id=? WHERE old_id=?`,
			binding.DeviceID, binding.ResidentID); err != nil {
			return err
		}
	}
	if binding.ExistingFamilyID == nil && binding.FamilyID != nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE oldperson SET family_id=? WHERE old_id=?`,
			*binding.FamilyID, binding.ResidentID); err != nil {
			return err
		}
	}
	if err := telemetryrepo.EnsureDefaultWatchProfile(ctx, tx, binding.DeviceID, binding.ProfileDay); err != nil {
		return err
	}
	result, err = tx.ExecContext(ctx,
		`UPDATE bind_code SET used=1, used_device_id=?, used_time=?
		 WHERE id=? AND used=0`,
		binding.DeviceID, binding.At, binding.CodeID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrBindCodeUsed
	}
	return nil
}
