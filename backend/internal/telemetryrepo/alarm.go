package telemetryrepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/locationalertstate"
)

type Location struct {
	DeviceID         int64
	ResidentID       int64
	Longitude        string
	Latitude         string
	Source           string
	ObservedAt       time.Time
	PositionMetadata string
}

// InsertLocation appends a sealed device location within the caller's transaction.
func InsertLocation(ctx context.Context, tx *sql.Tx, location Location) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO device_location(device_id, old_id, lng, lat, source, be_time, create_time, position_metadata)
    VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP(6),?)`,
		location.DeviceID, location.ResidentID, location.Longitude, location.Latitude,
		location.Source, location.ObservedAt, location.PositionMetadata)
	return err
}

// UpdateLatestLocation prevents older observations from replacing current coordinates.
func UpdateLatestLocation(ctx context.Context, tx *sql.Tx, location Location) error {
	_, err := tx.ExecContext(ctx, `UPDATE device SET last_lng=?, last_lat=? WHERE device_id=?
	 AND NOT EXISTS(SELECT 1 FROM device_location WHERE device_id=? AND old_id=? AND be_time>?)`,
		location.Longitude, location.Latitude, location.DeviceID, location.DeviceID, location.ResidentID, location.ObservedAt)
	return err
}

type Alarm struct {
	ResidentID       int64
	DeviceID         int64
	ObservedAt       time.Time
	Longitude        any
	Latitude         any
	Kind             string
	PositionMetadata string
}

const insertAlarm = `INSERT INTO alarm_precess(status, old_id, device_id, be_time, lng, lat, kind, position_metadata)
 VALUES('1',?,?,?,?,?,?,?)`

// InsertAlarm writes an alarm and returns its persisted identifier.
func InsertAlarm(ctx context.Context, tx *sql.Tx, alarm Alarm) (int64, error) {
	id, err := db.InsertID(ctx, tx, insertAlarm, alarm.ResidentID, alarm.DeviceID,
		alarm.ObservedAt, alarm.Longitude, alarm.Latitude, alarm.Kind, alarm.PositionMetadata)
	if err != nil {
		return 0, err
	}
	if err := locationalertstate.Record(ctx, tx, alarm.ResidentID, id, alarm.ObservedAt); err != nil {
		return 0, err
	}
	return id, nil
}

type Incident struct {
	AlarmID    int64
	ResidentID int64
	DeviceID   int64
	Kind       string
	RequestID  any
}

// InsertIncident persists the care incident associated with an alarm.
func InsertIncident(ctx context.Context, tx *sql.Tx, incident Incident) (int64, error) {
	return db.InsertID(ctx, tx,
		`INSERT INTO care_incident(source_alarm_id,old_id,device_id,kind,request_id)
		 VALUES(?,?,?,?,?)`,
		incident.AlarmID, incident.ResidentID, incident.DeviceID,
		incident.Kind, incident.RequestID)
}

// InsertSOSEvent creates the required receipt evidence, not a delivery confirmation.
func InsertSOSEvent(ctx context.Context, tx *sql.Tx, incidentID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO care_incident_event(incident_id, actor, action, note)
	 VALUES(?,'device','received','SOS 已持久化，尚无通知送达或人工接单确认')`, incidentID)
	return err
}

// InsertAlarmEvent records a non-SOS alert before notification is queued.
func InsertAlarmEvent(ctx context.Context, tx *sql.Tx, incidentID int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO care_incident_event(incident_id,actor,action,note)
	 VALUES(?,'device','received','设备异常已持久化；等待通知和人工处置')`, incidentID)
	return err
}

// InsertSOSIncident retains the SOS-specific database default for incident kind.
func InsertSOSIncident(ctx context.Context, tx *sql.Tx, alarmID, residentID, deviceID int64, requestID any) (int64, error) {
	return db.InsertID(ctx, tx,
		`INSERT INTO care_incident(source_alarm_id, old_id, device_id, request_id)
		 VALUES(?,?,?,?)`,
		alarmID, residentID, deviceID, requestID)
}
