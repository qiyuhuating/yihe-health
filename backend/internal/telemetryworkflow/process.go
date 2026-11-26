package telemetryworkflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/devicerepo"
	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryrepo"
)

// Persist applies an authenticated queue payload to the resident's data.
// The resident and device row locks, plus binding-epoch validation, are held.
type Persist func(context.Context, *sql.Tx, *devicerepo.DeviceRecord, Payload) error

// ProcessItem owns stale-binding detection and keeps the same resident-first
// lock order as the interactive device reporting and SOS paths.
func ProcessItem(ctx context.Context, tx *sql.Tx, box *securestore.Box, item telemetryrepo.DurableItem, persist Persist) error {
	payload, err := Decode(box, item.Payload)
	if err != nil {
		return err
	}
	var owner int64
	err = tx.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE", item.ResidentID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return telemetryrepo.MarkDurableStale(ctx, tx, item.ID, "elder removed")
	}
	if err != nil {
		return err
	}
	device, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, item.DeviceID))
	if errors.Is(err, sql.ErrNoRows) {
		return telemetryrepo.MarkDurableStale(ctx, tx, item.ID, "device removed")
	}
	if err != nil {
		return err
	}
	if device.OldID == nil || *device.OldID != item.ResidentID || device.HealthEpoch != item.BindingEpoch {
		return telemetryrepo.MarkDurableStale(ctx, tx, item.ID, "device binding changed")
	}
	if err := persist(ctx, tx, device, payload); err != nil {
		return err
	}
	return telemetryrepo.MarkDurableDone(ctx, tx, item.ID, time.Now().UTC())
}
