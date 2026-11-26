package vitalrepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

type DeviceAlertState struct {
	DeviceID      int64
	DeviceNo      string
	Alias         *string
	ResidentID    *int64
	LastSeen      *time.Time
	Battery       *int
	BootSeq       int
	HandledSeq    int
	CreatedAt     *time.Time
	ResidentName  *string
	HRHigh, HRLow *int
	BPHigh, BPLow *int
}

const personCols = `d.device_id, d.device_no, d.alias, d.old_id, d.last_time, d.battery,
	d.boot_seq, d.handled_seq, d.create_time, o.name, w.hrf, w.hrl, w.bpf, w.bpl`
const personFrom = `FROM device d LEFT JOIN oldperson o ON o.old_id = d.old_id
	LEFT JOIN watch_info w ON w.device_id = d.device_id`

func scanPersonRow(row interface{ Scan(...any) error }, extra ...any) (*DeviceAlertState, error) {
	var pr DeviceAlertState
	dest := []any{&pr.DeviceID, &pr.DeviceNo, &pr.Alias, &pr.ResidentID, &pr.LastSeen,
		&pr.Battery, &pr.BootSeq, &pr.HandledSeq, &pr.CreatedAt, &pr.ResidentName,
		&pr.HRHigh, &pr.HRLow, &pr.BPHigh, &pr.BPLow}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &pr, nil
}

func FindDeviceAlertState(ctx context.Context, tx *sql.Tx, deviceID int64) (*DeviceAlertState, error) {
	return scanPersonRow(tx.QueryRowContext(ctx,
		`SELECT `+personCols+` `+personFrom+` WHERE d.device_id=?`, deviceID))
}

const deviceAlertCountsSQL = `SELECT ` + personCols + `, COALESCE(counts.rate_count,0), COALESCE(counts.pressure_count,0) ` + personFrom + `
		LEFT JOIN (SELECT r.device_id, ` + pendingCountColumns + `
			FROM ratepre r JOIN device active ON active.device_id=r.device_id AND active.status=1
			GROUP BY r.device_id) counts ON counts.device_id=d.device_id
		WHERE d.status=1 ORDER BY d.device_id`

func EachDeviceAlertCount(ctx context.Context, tx *sql.Tx, visit func(*DeviceAlertState, int64, int64)) error {
	return db.QueryEach(ctx, tx, func(rows *sql.Rows) error {
		var rate, pressure int64
		state, err := scanPersonRow(rows, &rate, &pressure)
		if err != nil {
			return err
		}
		visit(state, rate, pressure)
		return nil
	}, deviceAlertCountsSQL)
}
