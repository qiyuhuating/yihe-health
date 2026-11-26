package handlers

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/devicecredential"
)

// Health writes need six authoritative fields, not the UI profile, coordinates,
// counters or application metadata. Keep the same lookup keys, row-lock order,
// and credential policy as the full device repository.
type healthDevice struct {
	ID          int64
	No, Secret  string
	OldID       *int64
	Status      *int
	HealthEpoch int64
}

const healthDeviceCols = `device_id,device_no,secret,old_id,status,health_epoch`

func scanHealthDevice(row *sql.Row) (*healthDevice, error) {
	var d healthDevice
	if err := row.Scan(&d.ID, &d.No, &d.Secret, &d.OldID, &d.Status, &d.HealthEpoch); err != nil {
		return nil, err
	}
	return &d, nil
}
func (h *Handler) healthDeviceAuth(ctx context.Context, q db.RowQuerier, deviceNo, secret, disabledMsg string) (*healthDevice, string, string, error) {
	d, err := scanHealthDevice(q.QueryRowContext(ctx, `SELECT `+healthDeviceCols+` FROM device WHERE device_no=?`, deviceNo))
	if errors.Is(err, sql.ErrNoRows) {
		d = nil
		err = nil
	}
	if err != nil {
		return nil, "", "", err
	}
	if d == nil {
		return nil, "DEVICE_AUTH_FAILED", "设备号或密钥不正确", nil
	}
	switch devicecredential.Check(secret, d.Secret, d.Status) {
	case devicecredential.InvalidSecret:
		return nil, "DEVICE_AUTH_FAILED", "设备号或密钥不正确", nil
	case devicecredential.Disabled:
		return nil, "DEVICE_DISABLED", disabledMsg, nil
	default:
		return d, "", "", nil
	}
}

// Health requests never supply battery/appVersion. Avoid transmitting nil
// parameters and evaluating COALESCE for columns whose values stay untouched.
// Two equal timestamp arguments keep positional binding ordered on both engines.
func touchHealthDevice(ctx context.Context, q *sql.Tx, deviceID int64, now time.Time) error {
	_, err := q.ExecContext(ctx, `UPDATE device SET last_time=?,last_online_at=? WHERE device_id=?`, now, now, deviceID)
	return err
}
