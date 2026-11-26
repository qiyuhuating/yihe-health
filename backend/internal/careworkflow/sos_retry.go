package careworkflow

import (
	"context"
	"database/sql"
	"errors"
)

type SOSRetry struct {
	ID                int64
	ResidentID        int64
	Kind              string
	State             string
	NotificationState string
}

// FindSOSRetry locks an existing request before a device retry is acknowledged.
// The caller holds the elder/device locks and owns the surrounding transaction.
func FindSOSRetry(ctx context.Context, tx *sql.Tx, deviceID int64, requestID string) (*SOSRetry, error) {
	var found SOSRetry
	err := tx.QueryRowContext(ctx, `SELECT i.id,i.old_id,i.kind,i.state,
		(SELECT `+notificationStatusPrioritySQL+` FROM care_notification_outbox n
		WHERE n.incident_id=i.id)
		FROM care_incident i WHERE i.device_id=? AND i.request_id=? FOR UPDATE`,
		deviceID, requestID).Scan(&found.ID, &found.ResidentID, &found.Kind, &found.State, &found.NotificationState)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &found, nil
}
