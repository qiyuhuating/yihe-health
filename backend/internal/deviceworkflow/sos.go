package deviceworkflow

import (
	"context"
	"database/sql"

	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/notify"
	"yihe-care/internal/securestore"
)

const (
	SOSRequestConflict Failure = "SOS_REQUEST_CONFLICT"
	NonceRejected      Failure = "NONCE_REJECTED"
)

type SOSInput struct {
	Number    string
	Secret    string
	Epoch     int64
	RequestID string
}

type SOSResult struct {
	IncidentID        int64
	IncidentState     string
	NotificationState string
	Duplicate         bool
}

// SOSHooks connect the signed-request boundary and incident encoder to the transaction.
// A rejected nonce must write its own response; the transaction is rolled back.
type SOSHooks struct {
	AcceptNonce func(context.Context, *sql.Tx, int64) bool
	Persist     func(context.Context, *sql.Tx, *devicerepo.DeviceRecord) (int64, error)
}

// SOS owns lock ordering, retry lookup, incident persistence, outbox enqueue and commit.
func SOS(ctx context.Context, store *db.Database, box *securestore.Box, input SOSInput, hooks SOSHooks) (SOSResult, Failure, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SOSResult{}, "", err
	}
	defer tx.Rollback()

	device, failure, err := LockBoundDevice(ctx, tx, input.Number, input.Secret, input.Epoch)
	if err != nil || device == nil {
		return SOSResult{}, failure, err
	}
	if input.RequestID != "" {
		prior, err := careworkflow.FindSOSRetry(ctx, tx, device.ID, input.RequestID)
		if err != nil {
			return SOSResult{}, "", err
		}
		if prior != nil {
			if prior.ResidentID != *device.OldID || prior.Kind != "sos" {
				return SOSResult{}, SOSRequestConflict, nil
			}
			return SOSResult{
				IncidentID: prior.ID, IncidentState: prior.State,
				NotificationState: prior.NotificationState, Duplicate: true,
			}, "", nil
		}
	}
	if !hooks.AcceptNonce(ctx, tx, device.ID) {
		return SOSResult{}, NonceRejected, nil
	}
	incidentID, err := hooks.Persist(ctx, tx, device)
	if err != nil {
		return SOSResult{}, "", err
	}
	notificationState, err := notify.EnqueueIncident(ctx, tx, box, incidentID, *device.OldID)
	if err != nil {
		return SOSResult{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return SOSResult{}, "", err
	}
	return SOSResult{IncidentID: incidentID, IncidentState: "received", NotificationState: notificationState}, "", nil
}
