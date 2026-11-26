package careworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/carerepo"
	"yihe-care/internal/db"
)

var (
	ErrStaffMissing = errors.New("care staff account not found")
	ErrDutyDenied   = errors.New("care duty assignment denied")
	ErrSelfDemotion = errors.New("care supervisor cannot demote self")
)

// AssignDuty owns authorization, the permission mutation and its audit commit.
// Locking the actor's permission row prevents a concurrent demotion from racing
// an already-authorized assignment.
func AssignDuty(ctx context.Context, store *db.Database, actor, target int64, duty string) error {
	if actor < 1 || target < 1 || (duty != "supervisor" && duty != "caregiver" && duty != "auditor" && duty != "unassigned") {
		return ErrDutyDenied
	}
	if actor == target && duty != "supervisor" {
		return ErrSelfDemotion
	}
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := carerepo.LockAuthority(ctx, tx); err != nil {
		return err
	}
	actorDuty, err := carerepo.LockDuty(ctx, tx, actor)
	if err != nil {
		return err
	}
	if actorDuty != "supervisor" {
		return ErrDutyDenied
	}
	exists, err := carerepo.AccountExists(ctx, tx, target)
	if err != nil {
		return err
	}
	if !exists {
		return ErrStaffMissing
	}
	if err := carerepo.SetDuty(ctx, tx, target, actor, duty); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO care_access_audit(admin_id,action,subject_admin_id)
 VALUES(?,'staff_duty_changed',?)`, actor, target); err != nil {
		return err
	}
	return tx.Commit()
}
