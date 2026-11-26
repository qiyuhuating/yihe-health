package fencerepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/fence"
)

type NewEvent struct {
	FenceID       int64
	FenceRevision int64
	ResidentID    int64
	DeviceID      int64
	Epoch         int64
	Direction     string
	Point         fence.Point
	Accuracy      float64
	OccurredAt    time.Time
}

func InsertEvent(ctx context.Context, tx *sql.Tx, event NewEvent) (int64, error) {
	return db.InsertID(ctx, tx,
		`INSERT INTO fence_event(fence_id,fence_revision,old_id,device_id,epoch,direction,lng,lat,accuracy,occurred_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		event.FenceID, event.FenceRevision, event.ResidentID, event.DeviceID, event.Epoch,
		event.Direction, event.Point[0], event.Point[1], event.Accuracy, event.OccurredAt)
}

func InsertExitIncident(ctx context.Context, tx *sql.Tx, residentID, deviceID int64) (int64, error) {
	return db.InsertID(ctx, tx,
		"INSERT INTO care_incident(old_id,device_id,kind,severity) VALUES(?,?,'fence_exit','high')",
		residentID, deviceID)
}

func InsertExitReceipt(ctx context.Context, tx *sql.Tx, incidentID int64) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(?,'rule','received','围栏离开事件，等待通知与照护处置')",
		incidentID)
	return err
}

func LinkIncident(ctx context.Context, tx *sql.Tx, eventID, incidentID int64) error {
	_, err := tx.ExecContext(ctx, "UPDATE fence_event SET incident_id=? WHERE id=?", incidentID, eventID)
	return err
}
