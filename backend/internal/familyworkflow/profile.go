package familyworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/blob"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/familyrepo"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

var (
	ErrWrongPassword   = errors.New("family phone or password mismatch")
	ErrPasswordChanged = errors.New("family password changed during verification")
)

// scopedProfile locks the live administrator duty or the current family identity
// before locking the target family row. Its caller owns the returned transaction.
func scopedProfile(ctx context.Context, store *db.Database, box *securestore.Box, actor session.Principal, targetID int64, phone string) (*sql.Tx, int64, string, error) {
	var tx *sql.Tx
	var err error
	switch actor.Role {
	case auth.RoleAdmin:
		tx, _, err = careworkflow.BeginAuthorized(ctx, store, actor.ID, carepolicy.ManageFamilies)
	case auth.RoleFamily:
		if actor.ID < 1 || actor.Phone != phone || (targetID > 0 && targetID != actor.ID) {
			return nil, 0, "", careworkflow.ErrCapabilityDenied
		}
		tx, err = store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if targetID == 0 {
			targetID = actor.ID
		}
	default:
		return nil, 0, "", careworkflow.ErrCapabilityDenied
	}
	if err != nil {
		return nil, 0, "", err
	}
	id, currentPhone, err := familyrepo.LockProfile(ctx, tx, box, targetID, phone)
	if errors.Is(err, familyrepo.ErrFamilyNotFound) && actor.Role == auth.RoleFamily {
		err = careworkflow.ErrCapabilityDenied
	}
	if err == nil && actor.Role == auth.RoleFamily && currentPhone != actor.Phone {
		err = careworkflow.ErrCapabilityDenied
	}
	if err != nil {
		tx.Rollback()
		return nil, 0, "", err
	}
	return tx, id, currentPhone, nil
}

// UpdateProfile returns whether a row changed and whether the family's phone
// identity changed, so all in-memory sessions can be invalidated after commit.
func UpdateProfile(ctx context.Context, store *db.Database, box *securestore.Box, actor session.Principal, id int64, phone, name, sex, address string) (bool, bool, error) {
	tx, id, currentPhone, err := scopedProfile(ctx, store, box, actor, id, phone)
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()
	updated, err := familyrepo.UpdateProfileDetails(ctx, tx, box, id, currentPhone, name, sex, address, phone)
	if err != nil {
		return false, false, err
	}
	phoneChanged := currentPhone != phone
	if phoneChanged {
		if err := authrepo.RevokeDatabaseSessions(ctx, tx, auth.RoleFamily, id); err != nil {
			return false, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, false, err
	}
	return updated, phoneChanged, nil
}

func ChangeAvatar(ctx context.Context, store *db.Database, box *securestore.Box, actor session.Principal, phone, image string, manager *blob.Manager) (bool, error) {
	tx, id, _, err := scopedProfile(ctx, store, box, actor, 0, phone)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := manager.Require(ctx, tx, []string{image}); err != nil {
		return false, err
	}
	changed, err := familyrepo.UpdateAvatar(ctx, tx, id, image)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

// ChangePassword keeps expensive password verification outside the row lock.
// The locked hash is rechecked before the replacement and session revocation.
func ChangePassword(ctx context.Context, store *db.Database, box *securestore.Box, actor session.Principal, phone, oldPassword, newPassword string) (int64, error) {
	if actor.Role != auth.RoleAdmin && (actor.Role != auth.RoleFamily || actor.Phone != phone) {
		return 0, careworkflow.ErrCapabilityDenied
	}
	previous, err := familyrepo.PasswordByPhone(ctx, store, box, phone)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (previous == nil || !auth.Matches(oldPassword, *previous))) {
		return 0, ErrWrongPassword
	}
	if err != nil {
		return 0, err
	}
	hash, err := auth.Hash(newPassword)
	if err != nil {
		return 0, err
	}
	tx, id, _, err := scopedProfile(ctx, store, box, actor, 0, phone)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	current, err := familyrepo.LockedPassword(ctx, tx, id)
	if err != nil {
		return 0, err
	}
	if current == nil || *current != *previous {
		return 0, ErrPasswordChanged
	}
	if err := familyrepo.SetPassword(ctx, tx, id, hash); err != nil {
		return 0, err
	}
	if err := authrepo.RevokeDatabaseSessions(ctx, tx, auth.RoleFamily, id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}
