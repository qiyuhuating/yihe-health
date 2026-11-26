package careworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/db"
	"yihe-care/internal/notificationrepo"
)

// ListNotifications keeps the family account or live supervisor lock through
// the message query and commits before returning any private message.
func ListNotifications(ctx context.Context, store *db.Database, role string, actorID, familyID, after int64) ([]notificationrepo.Message, error) {
	if actorID < 1 || after < 0 {
		return nil, ErrCapabilityDenied
	}
	var tx *sql.Tx
	var err error
	switch role {
	case auth.RoleAdmin:
		if familyID < 1 {
			return nil, ErrCapabilityDenied
		}
		tx, _, err = BeginAuthorized(ctx, store, actorID, carepolicy.ReadMessages)
	case auth.RoleFamily:
		familyID = actorID
		tx, err = store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err == nil {
			var id int64
			err = tx.QueryRowContext(ctx, "SELECT family_id FROM family WHERE family_id=? FOR SHARE", actorID).Scan(&id)
			if errors.Is(err, sql.ErrNoRows) {
				err = ErrCapabilityDenied
			}
		}
	default:
		return nil, ErrCapabilityDenied
	}
	if tx != nil {
		defer tx.Rollback()
	}
	if err != nil {
		return nil, err
	}
	messages, err := notificationrepo.List(ctx, tx, familyID, after)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return messages, nil
}
