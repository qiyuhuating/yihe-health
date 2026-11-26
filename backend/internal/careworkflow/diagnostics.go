package careworkflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
)

// Diagnostics describes persisted care workload and worker liveness.
// Notification-provider configuration and analytics readiness are application concerns.
type Diagnostics struct {
	PendingNotifications int64
	FailedNotifications  int64
	StaleSending         int64
	OverdueIncidents     int64
	OldestOutstandingAt  *time.Time
	WorkerLastSeenAt     *time.Time
	WorkerStartedAt      *time.Time
	WorkerHealthy        bool
}

// ReadDiagnostics gathers the operational care projection without HTTP coupling.
func ReadDiagnostics(ctx context.Context, store db.RowQuerier, now time.Time) (Diagnostics, error) {
	var d Diagnostics
	err := store.QueryRowContext(ctx, `
 SELECT
 (SELECT count(*) FROM care_notification_outbox WHERE status='pending'),
 (SELECT count(*) FROM care_notification_outbox WHERE status='failed'),
 (SELECT count(*) FROM care_notification_outbox WHERE status='sending' AND last_attempt_at < TIMESTAMPADD(MINUTE,-2,CURRENT_TIMESTAMP(6))),
 (SELECT count(*) FROM care_incident WHERE state NOT IN ('verified') AND due_at < CURRENT_TIMESTAMP(6)),
 (SELECT min(created_at) FROM care_notification_outbox WHERE status IN ('pending','failed'))`).Scan(
		&d.PendingNotifications, &d.FailedNotifications, &d.StaleSending,
		&d.OverdueIncidents, &d.OldestOutstandingAt)
	if err != nil {
		return Diagnostics{}, err
	}
	err = store.QueryRowContext(ctx,
		"SELECT last_seen_at,started_at FROM care_worker_heartbeat WHERE singleton=true").
		Scan(&d.WorkerLastSeenAt, &d.WorkerStartedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Diagnostics{}, err
	}
	d.WorkerHealthy = d.WorkerLastSeenAt != nil && now.Sub(*d.WorkerLastSeenAt) < 30*time.Second
	return d, nil
}
