package devicealertworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/vitalrepo"
)

var (
	ErrDeviceMissing  = errors.New("device not found")
	ErrUnknownKind    = errors.New("unknown alert kind")
	ErrNoPendingAlert = errors.New("no pending alert")
)

// ReasonReader runs inside the same transaction so the response describes
// the pre-resolution state without a second, inconsistent snapshot.
type ReasonReader func(context.Context, *sql.Tx, int64, string, *int64) (string, error)

func Resolve(ctx context.Context, store *db.Database, actorID, deviceID int64, kind string, recordID *int64, reason ReasonReader) (string, error) {
	if kind != "rate" && kind != "pre" && kind != "person" {
		return "", ErrUnknownKind
	}
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actorID, carepolicy.HandleAlerts)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	exists, err := devicerepo.ExistsInTransaction(ctx, tx, deviceID)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", ErrDeviceMissing
	}
	before, err := reason(ctx, tx, deviceID, kind, recordID)
	if err != nil {
		return "", err
	}
	var rows int64
	switch kind {
	case "rate", "pre":
		rows, err = vitalrepo.ResolveDeviceAlert(ctx, tx, deviceID, kind, recordID)
	case "person":
		rows, err = devicerepo.AcknowledgePersonAlert(ctx, tx, deviceID)
	default:
		return "", ErrUnknownKind
	}
	if err != nil {
		return "", err
	}
	if rows < 1 {
		return "", ErrNoPendingAlert
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'device_alert_resolve')", actorID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return before, nil
}
