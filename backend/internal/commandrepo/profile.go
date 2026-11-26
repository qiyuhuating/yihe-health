package commandrepo

import (
	"context"
	"database/sql"
)

type Profile struct {
	WireType string
	Protocol string
	Enabled  bool
}

func ReadProfile(ctx context.Context, tx *sql.Tx, deviceID int64) (Profile, error) {
	var profile Profile
	err := tx.QueryRowContext(ctx, "SELECT wire_type,protocol,enabled FROM device_command_profile WHERE device_id=?", deviceID).
		Scan(&profile.WireType, &profile.Protocol, &profile.Enabled)
	return profile, err
}

func SaveProfile(ctx context.Context, tx *sql.Tx, actor, deviceID int64, profile Profile) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO device_command_profile(device_id,wire_type,protocol,enabled,updated_by) VALUES(?,?,?,?,?)
 AS new ON DUPLICATE KEY UPDATE wire_type=new.wire_type,protocol=new.protocol,enabled=new.enabled,updated_by=new.updated_by,updated_at=CURRENT_TIMESTAMP(6)`,
		deviceID, profile.WireType, profile.Protocol, profile.Enabled, actor)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE device_command SET status='cancelled' WHERE device_id=? AND status IN ('pending','sent')", deviceID)
	return err
}
