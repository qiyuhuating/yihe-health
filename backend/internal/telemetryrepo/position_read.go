package telemetryrepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

// PositionRow is a sealed database projection; coordinates and metadata are
// deliberately not decrypted by the persistence layer.
type PositionRow struct {
	Name             *string
	ResidentID       int64
	Sex              *string
	Longitude        *string
	Latitude         *string
	ObservedAt       *time.Time
	Status           *string
	AlarmTime        *time.Time
	AlarmLongitude   *string
	AlarmLatitude    *string
	PositionMetadata *string
	AlarmMetadata    *string
	AlarmKind        string
}

// Use the existing (old_id, be_time, id) indexes for two top-one probes per
// resident instead of ranking every historical location and alert row.
const latestPositionsSQL = `SELECT o.name, o.old_id, o.sex, l.lng, l.lat, l.be_time AS be_time,
	COALESCE(a.status, '0') AS status, a.be_time AS alarm_time, a.lng AS alarm_lng, a.lat AS alarm_lat,
	l.position_metadata AS location_metadata, a.position_metadata AS alarm_metadata, COALESCE(a.kind,'unknown') AS alarm_kind
	FROM oldperson o
	LEFT JOIN device_location l ON l.id = (
		SELECT d.id FROM device_location d WHERE d.old_id = o.old_id
		ORDER BY d.be_time DESC, d.id DESC LIMIT 1)
	LEFT JOIN alarm_precess a ON a.id = (
		SELECT al.id FROM alarm_precess al WHERE al.old_id = o.old_id
		ORDER BY al.be_time DESC, al.id DESC LIMIT 1)`

func scanPosition(rows *sql.Rows) (PositionRow, error) {
	var item PositionRow
	err := rows.Scan(&item.Name, &item.ResidentID, &item.Sex,
		&item.Longitude, &item.Latitude, &item.ObservedAt, &item.Status,
		&item.AlarmTime, &item.AlarmLongitude, &item.AlarmLatitude,
		&item.PositionMetadata, &item.AlarmMetadata, &item.AlarmKind)
	return item, err
}

func ListLatestPositions(ctx context.Context, store db.ReadQuerier, name string, page, pageSize int) ([]PositionRow, int64, error) {
	const filter = " WHERE (?='' OR o.name LIKE CONCAT('%',?,'%'))"
	var total int64
	if err := store.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson o"+filter, name, name).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := db.QueryList(ctx, store, scanPosition,
		latestPositionsSQL+filter+" ORDER BY be_time DESC, o.old_id LIMIT ? OFFSET ?",
		name, name, pageSize, (page-1)*pageSize)
	return items, total, err
}

func ListResidentPositions(ctx context.Context, store db.RowsQuerier, residentID int64) ([]PositionRow, error) {
	return db.QueryList(ctx, store, scanPosition,
		`SELECT o.name, o.old_id, o.sex, l.lng, l.lat,
			l.be_time AS be_time, COALESCE(a.status, '0') AS status, a.be_time AS alarm_time,
			a.lng AS alarm_lng, a.lat AS alarm_lat,
			l.position_metadata AS location_metadata, a.position_metadata AS alarm_metadata,
			COALESCE(a.kind,'unknown') AS alarm_kind
		 FROM oldperson o
		 LEFT JOIN (SELECT old_id, lng, lat, be_time, position_metadata FROM device_location
			WHERE old_id=? ORDER BY be_time DESC, id DESC LIMIT 1) l ON l.old_id=o.old_id
		 LEFT JOIN (SELECT old_id, status, be_time, lng, lat, kind, position_metadata
			FROM alarm_precess WHERE old_id=? ORDER BY be_time DESC, id DESC LIMIT 1) a ON a.old_id=o.old_id
		 WHERE o.old_id=?`, residentID, residentID, residentID)
}
