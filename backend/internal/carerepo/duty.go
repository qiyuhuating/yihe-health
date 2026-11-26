// Package carerepo owns persisted care-role authorization.
package carerepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
)

// Duty returns the explicit assignment. An absent record has no privileges.
func Duty(ctx context.Context, store db.RowQuerier, adminID int64) (string, error) {
	var duty string
	err := store.QueryRowContext(ctx, `SELECT p.duty FROM care_staff_permission p
 JOIN users u ON u.user_id=p.admin_id AND u.role='1' AND u.account_status='active'
 WHERE p.admin_id=?`, adminID).Scan(&duty)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return duty, err
}

// AccountAccess is a single-query coarse route prefilter. Business authorization
// still happens against live locked rows inside the operation transaction.
func AccountAccess(ctx context.Context, store db.RowQuerier, adminID int64) (string, bool, error) {
	var status string
	var duty sql.NullString
	err := store.QueryRowContext(ctx, `SELECT u.account_status,p.duty
 FROM users u LEFT JOIN care_staff_permission p ON p.admin_id=u.user_id
 WHERE u.user_id=? AND u.role='1'`, adminID).Scan(&status, &duty)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return duty.String, status == "active", nil
}

// LockDuty retains a shared lock on the actor's explicit role until the
// business transaction commits. Concurrent duty reassignment must wait.
func LockDuty(ctx context.Context, tx *sql.Tx, adminID int64) (string, error) {
	var duty string
	err := tx.QueryRowContext(ctx,
		`SELECT p.duty FROM care_staff_permission p
 JOIN users u ON u.user_id=p.admin_id AND u.role='1' AND u.account_status='active'
 WHERE p.admin_id=? FOR SHARE`, adminID).Scan(&duty)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return duty, err
}

// LockAuthority serializes assignments and local supervisor recovery with
// first-run account creation using the existing singleton metadata row.
func LockAuthority(ctx context.Context, tx *sql.Tx) error {
	var id int64
	return tx.QueryRowContext(ctx, "SELECT id FROM app_metadata WHERE id=1 FOR UPDATE").Scan(&id)
}

var ErrLastSupervisor = errors.New("cannot remove the last active care supervisor")

// SetDuty requires the authority singleton lock for changes to existing duties.
// It refuses to replace the last active supervisor, even if future callers
// relax the handler-level self-demotion restriction.
func SetDuty(ctx context.Context, tx *sql.Tx, target, actor int64, duty string) error {
	if duty != "supervisor" {
		var current string
		err := tx.QueryRowContext(ctx, "SELECT duty FROM care_staff_permission WHERE admin_id=? FOR UPDATE", target).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current == "supervisor" {
			var others int64
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM care_staff_permission p
 JOIN users u ON u.user_id=p.admin_id AND u.role='1' AND u.account_status='active'
 WHERE p.duty='supervisor' AND p.admin_id<>?`, target).Scan(&others)
			if err != nil {
				return err
			}
			if others == 0 {
				return ErrLastSupervisor
			}
		}
	}
	if duty == "unassigned" {
		_, err := tx.ExecContext(ctx, "DELETE FROM care_staff_permission WHERE admin_id=?", target)
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO care_staff_permission(admin_id,duty,assigned_by,assigned_at)
 VALUES(?,?,?,CURRENT_TIMESTAMP(6)) AS new ON DUPLICATE KEY UPDATE
 duty=new.duty,assigned_by=new.assigned_by,assigned_at=CURRENT_TIMESTAMP(6)`, target, duty, actor)
	return err
}

// AccountExists verifies that a duty target is an administrator account.
func AccountExists(ctx context.Context, tx *sql.Tx, target int64) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE user_id=? AND role='1' AND account_status='active')", target).Scan(&exists)
	return exists, err
}
