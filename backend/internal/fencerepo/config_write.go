package fencerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"yihe-care/internal/db"
)

var ErrFenceLimit = errors.New("too many fences for binding")
var ErrFenceRevision = errors.New("fence or binding revision conflict")

// Save runs under a previously acquired device row lock and remains in the
// same transaction as the caller's care-access audit.
func Save(ctx context.Context, tx *sql.Tx, actor int64, deviceID, residentID, epoch int64, config *Config) error {
	coordinates, err := json.Marshal(config.Coordinates)
	if err != nil {
		return err
	}
	if config.ID == 0 {
		var count int
		if err := tx.QueryRowContext(ctx,
			"SELECT count(*) FROM device_fence WHERE device_id=? AND epoch=?",
			deviceID, epoch).Scan(&count); err != nil {
			return err
		}
		if count >= 20 {
			return ErrFenceLimit
		}
		config.ID, err = db.InsertID(ctx, tx,
			`INSERT INTO device_fence(device_id,old_id,epoch,name,enabled,coordinates,coordinate_system,timezone,time_begin,time_end,min_points,dwell_seconds,max_accuracy,updated_by)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			deviceID, residentID, epoch, config.Name, config.Enabled, json.RawMessage(coordinates),
			config.CoordinateSystem, config.Timezone, config.TimeBegin, config.TimeEnd, config.MinPoints,
			config.DwellSeconds, config.MaxAccuracy, actor)
		if err != nil {
			return err
		}
		config.Revision = 1
		return nil
	}
	var revision int64
	err = tx.QueryRowContext(ctx,
		"SELECT revision FROM device_fence WHERE id=? AND device_id=? AND old_id=? AND epoch=? FOR UPDATE",
		config.ID, deviceID, residentID, epoch).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && revision != config.Revision) {
		return ErrFenceRevision
	}
	if err != nil {
		return err
	}
	config.Revision++
	if _, err := tx.ExecContext(ctx,
		`UPDATE device_fence SET name=?,enabled=?,coordinates=?,coordinate_system=?,timezone=?,time_begin=?,time_end=?,min_points=?,dwell_seconds=?,max_accuracy=?,updated_by=?,revision=?,updated_at=CURRENT_TIMESTAMP(6) WHERE id=?`,
		config.Name, config.Enabled, json.RawMessage(coordinates), config.CoordinateSystem, config.Timezone, config.TimeBegin, config.TimeEnd, config.MinPoints, config.DwellSeconds, config.MaxAccuracy, actor, config.Revision, config.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM fence_state WHERE fence_id=?", config.ID)
	return err
}
