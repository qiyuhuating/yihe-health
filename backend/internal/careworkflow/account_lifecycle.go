package careworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/accountrepo"
	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/db"
)

var (
	ErrAccountMissing    = errors.New("administrator account missing")
	ErrAccountRetired    = errors.New("retired administrator cannot be reactivated")
	ErrAccountSelfChange = errors.New("administrator cannot change own lifecycle state")
)

// ChangeAccountStatus serializes lifecycle changes against duty assignment and
// first-run recovery. All session invalidation and audit SQL commit atomically.
// In-memory session replicas also revoke the affected principal after commit.
func ChangeAccountStatus(ctx context.Context, store *db.Database, actorID, targetID int64, status string) error {
	if actorID < 1 || targetID < 1 ||
		(status != "active" && status != "suspended" && status != "retired") {
		return ErrDutyDenied
	}
	if actorID == targetID {
		return ErrAccountSelfChange
	}
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := carerepo.LockAuthority(ctx, tx); err != nil {
		return err
	}
	duty, err := carerepo.LockDuty(ctx, tx, actorID)
	if err != nil {
		return err
	}
	if !carepolicy.Allowed(carepolicy.Duty(duty), carepolicy.ManageAccounts) {
		return ErrDutyDenied
	}
	target, err := accountrepo.Lock(ctx, tx, targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountMissing
	}
	if err != nil {
		return err
	}
	if target.Role != "1" {
		return ErrAccountMissing
	}
	if target.Status == "retired" && status != "retired" {
		return ErrAccountRetired
	}
	if target.Status == status {
		return tx.Commit()
	}
	if target.Status == "active" && status != "active" {
		var assignedDuty string
		assignedDuty, err = carerepo.Duty(ctx, tx, targetID)
		if err != nil {
			return err
		}
		if assignedDuty == "supervisor" {
			others, err := accountrepo.ActiveSupervisorsOtherThan(ctx, tx, targetID)
			if err != nil {
				return err
			}
			if others == 0 {
				return carerepo.ErrLastSupervisor
			}
		}
	}
	if err := accountrepo.SetStatus(ctx, tx, targetID, status); err != nil {
		return err
	}
	if status == "retired" {
		if err := accountrepo.RetireDuty(ctx, tx, targetID); err != nil {
			return err
		}
	}
	if err := authrepo.RevokeDatabaseSessions(ctx, tx, auth.RoleAdmin, targetID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO care_access_audit(admin_id,action,subject_admin_id) VALUES(?,?,?)",
		actorID, "admin_account_"+status, targetID); err != nil {
		return err
	}
	return tx.Commit()
}
