// Package careworkflow owns incident state transitions and their transactional writes.
package careworkflow

import (
	"context"
	"database/sql"

	"yihe-care/internal/locationalertstate"
)

// NextState returns an empty string for an invalid incident transition.
func NextState(before, action string) string {
	switch before {
	case "received", "notifying", "notified":
		switch action {
		case "acknowledge":
			return "acknowledged"
		case "escalate":
			return "escalated"
		}
	case "escalated":
		if action == "acknowledge" {
			return "acknowledged"
		}
	case "review_overdue", "resolved":
		switch action {
		case "verify":
			return "verified"
		case "reopen":
			return "in_progress"
		}
	case "acknowledged":
		switch action {
		case "start":
			return "in_progress"
		case "escalate":
			return "escalated"
		}
	case "in_progress":
		switch action {
		case "resolve":
			return "resolved"
		case "escalate":
			return "escalated"
		}
	}
	return ""
}

// LockedIncidentState reads the transition preconditions while holding the row lock.
// The caller must keep the transaction open through the subsequent writes.
func LockedIncidentState(ctx context.Context, tx *sql.Tx, id int64) (state string, resolvedBy, acknowledgedBy *string, err error) {
	err = tx.QueryRowContext(ctx, "SELECT state,resolved_by,acknowledged_by FROM care_incident WHERE id=? FOR UPDATE", id).
		Scan(&state, &resolvedBy, &acknowledgedBy)
	return
}

// PersistReviewVerdict records the human review within the incident transition transaction.
func PersistReviewVerdict(ctx context.Context, tx *sql.Tx, id int64, verdict, note string, actor int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO alert_review(entity,record_id,kind,verdict,reason,actor_id)
 SELECT 'care',id,kind,?,?,? FROM care_incident WHERE id=?`, verdict, note, actor, id)
	return err
}

// PersistTransition writes the new state, related alarm status and event in one transaction.
// The caller owns the transaction and commits only after all related writes succeed.
func PersistTransition(ctx context.Context, tx *sql.Tx, id int64, next, action, actor, note string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE care_incident SET
    state=?,updated_at=CURRENT_TIMESTAMP(6),
    due_at=CASE
      WHEN ?='acknowledge' THEN TIMESTAMPADD(MINUTE,10,CURRENT_TIMESTAMP(6))
      WHEN ?='start' THEN TIMESTAMPADD(MINUTE,30,CURRENT_TIMESTAMP(6))
      WHEN ?='resolve' THEN TIMESTAMPADD(MINUTE,30,CURRENT_TIMESTAMP(6))
      WHEN ?='reopen' THEN TIMESTAMPADD(MINUTE,10,CURRENT_TIMESTAMP(6))
      ELSE due_at END,
    acknowledged_by=CASE WHEN ?='acknowledge' THEN ? ELSE acknowledged_by END,
    acknowledged_at=CASE WHEN ?='acknowledge' THEN CURRENT_TIMESTAMP(6) ELSE acknowledged_at END,
    resolved_by=CASE WHEN ?='resolve' THEN ? ELSE resolved_by END,
    resolved_at=CASE WHEN ?='resolve' THEN CURRENT_TIMESTAMP(6) ELSE resolved_at END,
    resolution_note=CASE WHEN ?='resolve' THEN ? ELSE resolution_note END,
    verified_by=CASE WHEN ?='verify' THEN ? ELSE verified_by END,
    verified_at=CASE WHEN ?='verify' THEN CURRENT_TIMESTAMP(6) ELSE verified_at END
   WHERE id=?`, next, action, action, action, action, action, actor, action, action, actor, action, action, note, action, actor, action, id); err != nil {
		return err
	}
	if action == "verify" {
		var residentID int64
		var alarmID *int64
		if err := tx.QueryRowContext(ctx, "SELECT old_id,source_alarm_id FROM care_incident WHERE id=?", id).Scan(&residentID, &alarmID); err != nil {
			return err
		}
		if alarmID != nil {
			if err := locationalertstate.Resolve(ctx, tx, residentID, *alarmID); err != nil {
				return err
			}
		}
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(?,?,?,?)",
		id, actor, action, note)
	return err
}
