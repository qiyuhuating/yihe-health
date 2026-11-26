package careworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/carerepo"
)

var ErrCapabilityDenied = errors.New("care capability denied")

// BeginAuthorized holds the current administrator role and explicit duty until
// the caller commits or rolls back. Acquire business resource locks only after
// this call, so revocation and resource operations use a consistent order.
func BeginAuthorized(ctx context.Context, store interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}, actor int64, action carepolicy.Action) (*sql.Tx, carepolicy.Duty, error) {
	return BeginAuthorizedWithIsolation(ctx, store, actor, action, sql.LevelReadCommitted)
}

// BeginAuthorizedWithIsolation retains live duty locking while allowing a
// read-only business operation to use a consistent committed data snapshot.
func BeginAuthorizedWithIsolation(ctx context.Context, store interface {
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}, actor int64, action carepolicy.Action, isolation sql.IsolationLevel) (*sql.Tx, carepolicy.Duty, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		return nil, "", err
	}
	duty, err := carerepo.LockDuty(ctx, tx, actor)
	if err != nil {
		tx.Rollback()
		return nil, "", err
	}
	if !carepolicy.Allowed(carepolicy.Duty(duty), action) {
		tx.Rollback()
		return nil, "", ErrCapabilityDenied
	}
	return tx, carepolicy.Duty(duty), nil
}
