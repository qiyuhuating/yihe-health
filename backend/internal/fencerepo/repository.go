// Package fencerepo owns persisted geofence definitions and events.
package fencerepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/fence"
)

type Config struct {
	ID               int64         `json:"id"`
	DeviceID         int64         `json:"deviceId"`
	Revision         int64         `json:"revision"`
	Name             string        `json:"name"`
	Enabled          bool          `json:"enabled"`
	Coordinates      []fence.Point `json:"coordinates"`
	CoordinateSystem string        `json:"coordinateSystem"`
	Timezone         string        `json:"timezone"`
	TimeBegin        int           `json:"timeBegin"`
	TimeEnd          int           `json:"timeEnd"`
	MinPoints        int           `json:"minPoints"`
	DwellSeconds     int           `json:"dwellSeconds"`
	MaxAccuracy      float64       `json:"maxAccuracy"`
}

type Event struct {
	ID            int64     `json:"id"`
	FenceID       int64     `json:"fenceId"`
	FenceRevision int64     `json:"fenceRevision"`
	DeviceID      int64     `json:"deviceId"`
	Direction     string    `json:"direction"`
	Longitude     float64   `json:"lng"`
	Latitude      float64   `json:"lat"`
	Accuracy      float64   `json:"accuracy"`
	OccurredAt    time.Time `json:"occurredAt"`
	IncidentID    *int64    `json:"incidentId"`
}

// ListConfigured selects only definitions belonging to the current device binding epoch.
func ListConfigured(ctx context.Context, store db.RowsQuerier, deviceID int64) ([]Config, error) {
	return db.QueryList(ctx, store, func(rows *sql.Rows) (Config, error) {
		var item Config
		var data []byte
		if err := rows.Scan(&item.ID, &item.DeviceID, &item.Revision, &item.Name, &item.Enabled, &data,
			&item.CoordinateSystem, &item.Timezone, &item.TimeBegin, &item.TimeEnd,
			&item.MinPoints, &item.DwellSeconds, &item.MaxAccuracy); err != nil {
			return item, err
		}
		return item, json.Unmarshal(data, &item.Coordinates)
	}, `SELECT f.id,f.device_id,f.revision,f.name,f.enabled,f.coordinates,f.coordinate_system,f.timezone,
	 f.time_begin,f.time_end,f.min_points,f.dwell_seconds,f.max_accuracy
	 FROM device_fence f JOIN device d ON d.device_id=f.device_id AND d.old_id=f.old_id AND d.health_epoch=f.epoch
	 WHERE f.device_id=? ORDER BY f.id`, deviceID)
}

// ListActive locks all enabled definitions until the caller commits its observation transaction.
func ListActive(ctx context.Context, tx *sql.Tx, deviceID, residentID, epoch int64) ([]Config, error) {
	return db.QueryList(ctx, tx, func(rows *sql.Rows) (Config, error) {
		var item Config
		var data []byte
		if err := rows.Scan(&item.ID, &item.Revision, &data, &item.Timezone,
			&item.TimeBegin, &item.TimeEnd, &item.MinPoints, &item.DwellSeconds,
			&item.MaxAccuracy); err != nil {
			return item, err
		}
		return item, json.Unmarshal(data, &item.Coordinates)
	}, `SELECT id,revision,coordinates,timezone,time_begin,time_end,min_points,dwell_seconds,max_accuracy
	 FROM device_fence WHERE device_id=? AND old_id=? AND epoch=? AND enabled=true ORDER BY id FOR UPDATE`,
		deviceID, residentID, epoch)
}

func ListEvents(ctx context.Context, store db.RowsQuerier, residentID, after int64) ([]Event, error) {
	return db.QueryList(ctx, store, func(rows *sql.Rows) (Event, error) {
		var item Event
		err := rows.Scan(&item.ID, &item.FenceID, &item.FenceRevision, &item.DeviceID,
			&item.Direction, &item.Longitude, &item.Latitude, &item.Accuracy, &item.OccurredAt,
			&item.IncidentID)
		return item, err
	}, `SELECT id,fence_id,fence_revision,device_id,direction,lng,lat,accuracy,occurred_at,incident_id
	 FROM fence_event WHERE old_id=? AND id>? ORDER BY id LIMIT 100`, residentID, after)
}
