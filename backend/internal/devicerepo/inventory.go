// Package devicerepo persists device inventory and ownership mutations.
package devicerepo

import (
	"context"
	"database/sql"

	"yihe-care/internal/db"
)

// deviceRecordColumns is shared only inside the device persistence package.
const deviceRecordColumns = `device_id, device_no, secret, alias, model, app_version, battery,
	old_id, family_id, status, bind_time, last_time, last_lng, last_lat, create_time,
	person_handled, rate_handled, pre_handled, boot_seq, handled_seq, health_epoch`

func Exists(ctx context.Context, store db.RowQuerier, deviceID int64) (bool, error) {
	var exists bool
	err := store.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device WHERE device_id=?)`, deviceID).Scan(&exists)
	return exists, err
}

// NumberExists checks the unique device number in the inventory store.
func NumberExists(ctx context.Context, store *db.Database, number string) (bool, error) {
	var exists bool
	err := store.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device WHERE device_no=?)`, number).Scan(&exists)
	return exists, err
}

func UpdateSecret(ctx context.Context, store *sql.Tx, deviceID int64, hash string) (bool, error) {
	result, err := store.ExecContext(ctx, `UPDATE device SET secret=? WHERE device_id=?`, hash, deviceID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// Detach transfers the elder's primary device reference before removing the binding.
// All writes commit together, including profile deletion when remove is true.
func Detach(ctx context.Context, tx *sql.Tx, deviceID int64, remove bool) (bool, error) {

	if _, err := tx.ExecContext(ctx, `UPDATE oldperson SET device_id=(
		SELECT min(d.device_id) FROM device d
		WHERE d.old_id=oldperson.old_id AND d.device_id<>?
	) WHERE device_id=?`, deviceID, deviceID); err != nil {
		return false, err
	}

	query := `UPDATE device SET old_id=NULL, family_id=NULL, bind_time=NULL, last_lng=NULL, last_lat=NULL
		WHERE device_id=?`
	if remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM watch_info WHERE device_id=?`, deviceID); err != nil {
			return false, err
		}
		query = `DELETE FROM device WHERE device_id=?`
	}
	result, err := tx.ExecContext(ctx, query, deviceID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, nil
	}
	return true, nil
}
