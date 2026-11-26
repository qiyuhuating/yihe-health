// Package familyworkflow owns family account mutations and their role fence.
package familyworkflow

import (
	"context"
	"database/sql"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/familyrepo"
	"yihe-care/internal/securestore"
)

func transact(ctx context.Context, store *db.Database, actor int64, action string, mutate func(*sql.Tx) error) error {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ManageFamilies)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := mutate(tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action) VALUES(?,?)", actor, action); err != nil {
		return err
	}
	return tx.Commit()
}

func Create(ctx context.Context, store *db.Database, box *securestore.Box, actor int64, family familyrepo.Family, oldPhone string) error {
	return transact(ctx, store, actor, "family_create", func(tx *sql.Tx) error {
		return familyrepo.Create(ctx, tx, box, family, oldPhone)
	})
}

func Update(ctx context.Context, store *db.Database, box *securestore.Box, actor, familyID int64, family familyrepo.Family) error {
	return transact(ctx, store, actor, "family_update", func(tx *sql.Tx) error {
		return familyrepo.Update(ctx, tx, box, familyID, family)
	})
}

func Delete(ctx context.Context, store *db.Database, actor, familyID int64) error {
	return transact(ctx, store, actor, "family_delete", func(tx *sql.Tx) error {
		return familyrepo.Delete(ctx, tx, familyID)
	})
}
