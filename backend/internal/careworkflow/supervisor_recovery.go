package careworkflow

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"yihe-care/internal/carerepo"
	"yihe-care/internal/db"
	"yihe-care/internal/securityaudit"
)

var ErrRecoveryDenied = errors.New("supervisor recovery denied")

type RecoveryResult struct {
	AuditID  string
	Decision string
}

// RecoverSupervisor is local-only. The authority lock serializes recovery with
// normal duty and account lifecycle changes. Denied attempts commit evidence too.
func RecoverSupervisor(ctx context.Context, store *db.Database, adminID int64, actor securityaudit.Actor, reason string) (RecoveryResult, error) {
	if actor.User == "" || actor.UID == "" || actor.Host == "" {
		return RecoveryResult{}, errors.New("OS operator identity is required")
	}
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RecoveryResult{}, err
	}
	defer tx.Rollback()
	if err := carerepo.LockAuthority(ctx, tx); err != nil {
		return RecoveryResult{}, err
	}
	before := securityaudit.Snapshot{Duty: "unassigned"}
	err = tx.QueryRowContext(ctx, `SELECT u.role,u.account_status,COALESCE(p.duty,'unassigned')
 FROM users u LEFT JOIN care_staff_permission p ON p.admin_id=u.user_id
 WHERE u.user_id=? FOR UPDATE`, adminID).Scan(&before.Role, &before.Status, &before.Duty)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return RecoveryResult{}, err
	}
	before.Exists = err == nil
	decision := "granted"
	switch {
	case strings.TrimSpace(reason) == "":
		decision = "reason_required"
	case !before.Exists || before.Role != "1" || before.Status != "active":
		decision = "target_not_active_admin"
	default:
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM care_staff_permission p
 JOIN users u ON u.user_id=p.admin_id AND u.role='1' AND u.account_status='active'
 WHERE p.duty='supervisor'`).Scan(&existing); err != nil {
			return RecoveryResult{}, err
		}
		if existing != 0 {
			decision = "supervisor_exists"
		}
	}
	after, outcome := before, "denied"
	if decision == "granted" {
		if err := carerepo.SetDuty(ctx, tx, adminID, adminID, "supervisor"); err != nil {
			return RecoveryResult{}, err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action,subject_admin_id) VALUES(?,'supervisor_local_bootstrap',?)", adminID, adminID); err != nil {
			return RecoveryResult{}, err
		}
		after.Duty, outcome = "supervisor", "granted"
	}
	auditID, err := securityaudit.Append(ctx, tx, securityaudit.Entry{
		Actor: actor, AdminID: adminID, Reason: reason, Outcome: outcome,
		Decision: decision, Before: before, After: after,
	})
	if err != nil {
		return RecoveryResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{AuditID: auditID, Decision: decision}
	if decision != "granted" {
		return result, ErrRecoveryDenied
	}
	return result, nil
}
