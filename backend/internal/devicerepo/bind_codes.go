package devicerepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
)

type BindCode struct {
	ID           int64
	Code         string
	OldID        *int64
	FamilyID     *int64
	CreateUser   *string
	ExpireTime   *time.Time
	Used         int
	UsedTime     *time.Time
	UsedDeviceID *int64
}

type ResidentBinding struct {
	Name     *string
	DeviceID *int64
	FamilyID *int64
}

func LockResidentBinding(ctx context.Context, store db.RowQuerier, residentID int64) (ResidentBinding, error) {
	var binding ResidentBinding
	err := store.QueryRowContext(ctx,
		"SELECT name, device_id, family_id FROM oldperson WHERE old_id=? FOR UPDATE",
		residentID).Scan(&binding.Name, &binding.DeviceID, &binding.FamilyID)
	return binding, err
}

// FindBindCode returns nil for missing codes so the device protocol can
// distinguish unknown, consumed and expired binding codes without leaking SQL.
func FindBindCode(ctx context.Context, store db.RowQuerier, code string) (*BindCode, error) {
	var row BindCode
	err := store.QueryRowContext(ctx,
		`SELECT id,code,old_id,family_id,create_user,expire_time,used,used_time,used_device_id
		 FROM bind_code WHERE code=?`, code).
		Scan(&row.ID, &row.Code, &row.OldID, &row.FamilyID, &row.CreateUser,
			&row.ExpireTime, &row.Used, &row.UsedTime, &row.UsedDeviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func CodeExists(ctx context.Context, store db.RowQuerier, code string) (bool, error) {
	var exists bool
	err := store.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM bind_code WHERE code=?)", code).Scan(&exists)
	return exists, err
}

func ResidentName(ctx context.Context, store db.RowQuerier, residentID int64) (*string, error) {
	var name *string
	err := store.QueryRowContext(ctx, "SELECT name FROM oldperson WHERE old_id=?", residentID).Scan(&name)
	return name, err
}

func CreateBindCode(ctx context.Context, store *sql.Tx, code string, residentID int64, creator string, createdAt, expiresAt time.Time) (int64, error) {
	return db.InsertID(ctx, store,
		`INSERT INTO bind_code(code, old_id, family_id, create_user, create_time, expire_time, used)
		 VALUES(?,?,NULL,?,?,?,0)`,
		code, residentID, creator, createdAt, expiresAt)
}

func ListBindCodes(ctx context.Context, store db.RowsQuerier, residentID int64) ([]BindCode, error) {
	return db.QueryList(ctx, store, func(rows *sql.Rows) (BindCode, error) {
		var item BindCode
		err := rows.Scan(&item.ID, &item.Code, &item.ExpireTime, &item.Used, &item.UsedTime, &item.UsedDeviceID)
		return item, err
	}, `SELECT id,code,expire_time,used,used_time,used_device_id
	 FROM bind_code WHERE old_id=? ORDER BY id DESC LIMIT 20`, residentID)
}

func DeleteUnusedBindCode(ctx context.Context, store *sql.Tx, id int64) (bool, error) {
	affected, err := store.ExecContext(ctx, "DELETE FROM bind_code WHERE id=? AND used=0", id)
	if err != nil {
		return false, err
	}
	changed, err := affected.RowsAffected()
	return changed == 1, err
}
