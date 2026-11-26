package vitalrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

type PendingDeviceAlert struct {
	ID           int64
	HR           *int64
	BP           *int64
	OccurredAt   *time.Time
	ResidentID   *int64
	ResidentName *string
	HRHigh       *int
	HRLow        *int
	BPHigh       *int
	BPLow        *int
}

// FindPendingDeviceAlert reads the newest unresolved measurement for a device.
// It does not format text for HTTP, keeping the persisted measurement separate
// from the response and operator-facing explanation.
func FindPendingDeviceAlert(ctx context.Context, store db.RowQuerier, box *securestore.Box, deviceID int64, kind string, recordID *int64) (*PendingDeviceAlert, error) {
	pending := pendingPressure
	if kind == "rate" {
		pending = pendingRate
	}
	args := []any{deviceID}
	filter := ""
	if recordID != nil {
		filter = " AND r.id=?"
		args = append(args, *recordID)
	}
	var alert PendingDeviceAlert
	var stored StoredMeasurements
	var owner *int64
	err := store.QueryRowContext(ctx,
		`SELECT r.id, r.hr, r.bp, r.measurement_cipher, r.old_id, r.create_time, o.old_id, o.name, w.hrf, w.hrl, w.bpf, w.bpl
		 FROM ratepre r LEFT JOIN watch_info w ON w.device_id=r.device_id
		 LEFT JOIN oldperson o ON o.old_id=r.old_id
		 WHERE r.device_id=? AND `+pending+filter+`
		 ORDER BY r.create_time DESC, r.id DESC LIMIT 1`, args...).
		Scan(&alert.ID, &stored.HeartRate, &stored.Pressure, &stored.Cipher, &owner, &alert.OccurredAt,
			&alert.ResidentID, &alert.ResidentName, &alert.HRHigh,
			&alert.HRLow, &alert.BPHigh, &alert.BPLow)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	values, err := stored.Open(box, alert.ID, owner, &deviceID)
	if err != nil {
		return nil, err
	}
	alert.HR, alert.BP = values.HeartRate, values.Pressure
	return &alert, nil
}
