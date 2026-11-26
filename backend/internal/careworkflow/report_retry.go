package careworkflow

import (
	"context"
	"database/sql"
	"errors"
)

type ReportRetry struct {
	ID         int64
	ResidentID int64
	Kind       string
	State      string
}

// FindReportRetry reads a previously committed device alarm under the
// caller-owned binding and device locks. It does not repeat the notification
// aggregation needed only for SOS acknowledgements.
func FindReportRetry(ctx context.Context, tx *sql.Tx, deviceID int64, requestID string) (*ReportRetry, error) {
	var prior ReportRetry
	err := tx.QueryRowContext(ctx,
		`SELECT id,old_id,kind,state FROM care_incident
		 WHERE device_id=? AND request_id=? FOR UPDATE`,
		deviceID, requestID).Scan(&prior.ID, &prior.ResidentID, &prior.Kind, &prior.State)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &prior, nil
}
