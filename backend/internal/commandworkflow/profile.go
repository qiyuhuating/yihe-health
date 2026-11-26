package commandworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/commandrepo"
	"yihe-care/internal/db"
	"yihe-care/internal/devicerepo"
)

// Configure serializes profile changes with creation and delivery using the
// device row. Replaying the current profile must not revoke queued commands.
func Configure(ctx context.Context, store *db.Database, actor, deviceID int64, profile commandrepo.Profile) error {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ConfigureCommands)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := devicerepo.LockDeviceID(ctx, tx, deviceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrDeviceMissing
		}
		return err
	}
	current, err := commandrepo.ReadProfile(ctx, tx, deviceID)
	if err == nil && current == profile {
		return tx.Commit()
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := commandrepo.SaveProfile(ctx, tx, actor, deviceID, profile); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'command_profile')", actor); err != nil {
		return err
	}
	return tx.Commit()
}
