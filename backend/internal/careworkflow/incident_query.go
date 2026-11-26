package careworkflow

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/db"
)

// Incident is the typed read model for an operational care task.
type Incident struct {
	ID                int64      `json:"id"`
	ResidentID        int64      `json:"oldId"`
	DeviceID          int64      `json:"deviceId"`
	Kind              string     `json:"kind"`
	State             string     `json:"state"`
	Severity          string     `json:"severity"`
	CreatedAt         time.Time  `json:"createdAt"`
	DueAt             time.Time  `json:"dueAt"`
	ResidentName      *string    `json:"oldPersonName"`
	NotificationState string     `json:"notificationState"`
	AcknowledgedBy    *string    `json:"acknowledgedBy"`
	AcknowledgedAt    *time.Time `json:"acknowledgedAt"`
	ResolvedBy        *string    `json:"resolvedBy"`
	ResolvedAt        *time.Time `json:"resolvedAt"`
	VerifiedBy        *string    `json:"verifiedBy"`
	VerifiedAt        *time.Time `json:"verifiedAt"`
}

const notificationStatusPrioritySQL = `CASE
            WHEN MAX(n.status='delivered') THEN 'delivered'
            WHEN MAX(n.status='failed') THEN 'failed'
            WHEN MAX(n.status='sending') THEN 'sending'
            WHEN MAX(n.status='pending') THEN 'pending'
            WHEN MAX(n.status='sent') THEN 'sent'
            ELSE 'no_recipient' END`

const notificationStatusSQL = `(SELECT ` + notificationStatusPrioritySQL +
	` FROM care_notification_outbox n WHERE n.incident_id=i.id)`

const activeIncidentOrderSQL = ` ORDER BY
	CASE
		WHEN i.kind IN ('sos','fall') OR i.severity='critical' THEN 0
		WHEN i.severity IN ('high','warning') THEN 1
		ELSE 2
	END,
	(i.due_at < CURRENT_TIMESTAMP(6)) DESC,
	i.due_at ASC,
	i.id DESC`

const verifiedIncidentOrderSQL = ` ORDER BY i.verified_at DESC, i.id DESC`

// Caregivers see work available for pickup or already accepted by them.
// Escalated work is available for reassignment even when a previous owner remains recorded.
const caregiverIncidentScopeSQL = ` AND i.visibility_group IN ('PUBLIC_POOL', ?)`

func incidentReadScope(duty carepolicy.Duty, actorID int64) (string, []any) {
	switch duty {
	case carepolicy.Supervisor, carepolicy.Auditor:
		return "", nil
	case carepolicy.Caregiver:
		return caregiverIncidentScopeSQL, []any{"ASSIGNED:" + strconv.FormatInt(actorID, 10)}
	default:
		return " AND FALSE", nil
	}
}

// CanReadIncident applies the same row-level visibility as the paginated queue.
// Both missing and out-of-scope incidents are invisible to the caller.
func CanReadIncident(ctx context.Context, tx *sql.Tx, duty carepolicy.Duty, actorID, incidentID int64) (bool, error) {
	scope, scopeArgs := incidentReadScope(duty, actorID)
	args := append([]any{incidentID}, scopeArgs...)
	var id int64
	err := tx.QueryRowContext(ctx,
		`SELECT i.id FROM care_incident i WHERE i.id=?`+scope+` FOR SHARE`, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ListIncidents returns risk-ordered tasks and newest-first verified history
// inside the current actor's authorized visibility scope.
func ListIncidents(ctx context.Context, store *sql.Tx, duty carepolicy.Duty, actorID int64, verified bool, page, pageSize int) ([]Incident, int64, error) {
	scope, scopeArgs := incidentReadScope(duty, actorID)
	countArgs := append([]any{verified}, scopeArgs...)
	var total int64
	if err := store.QueryRowContext(ctx,
		`SELECT count(*) FROM care_incident i WHERE (i.state='verified')=?`+scope, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := activeIncidentOrderSQL
	if verified {
		order = verifiedIncidentOrderSQL
	}
	items, err := db.QueryList(ctx, store, func(rows *sql.Rows) (Incident, error) {
		var item Incident
		err := rows.Scan(&item.ID, &item.ResidentID, &item.DeviceID, &item.Kind, &item.State,
			&item.Severity, &item.CreatedAt, &item.DueAt, &item.AcknowledgedBy,
			&item.AcknowledgedAt, &item.ResolvedBy, &item.ResolvedAt,
			&item.VerifiedBy, &item.VerifiedAt, &item.ResidentName, &item.NotificationState)
		return item, err
	}, `SELECT i.id,i.old_id,i.device_id,i.kind,i.state,i.severity,i.created_at,i.due_at,
          i.acknowledged_by,i.acknowledged_at,i.resolved_by,i.resolved_at,i.verified_by,i.verified_at,
          o.name,
          `+notificationStatusSQL+`
     FROM care_incident i LEFT JOIN oldperson o ON o.old_id=i.old_id
     WHERE (i.state='verified')=?`+scope+order+`
     LIMIT ? OFFSET ?`, append(countArgs, pageSize, (page-1)*pageSize)...)
	return items, total, err
}
