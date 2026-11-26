package telemetryrepo

import (
	"context"
	"database/sql"
	"time"
)

type heartbeatWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// TouchDevice persists only observations from the current binding epoch.
// Out-of-order observations cannot roll back device state or metadata.
func TouchDevice(ctx context.Context, writer heartbeatWriter, deviceID, bindingEpoch int64, battery *int, appVersion *string, seenAt time.Time) error {
	var b, version any
	if battery != nil {
		b = *battery
	}
	if appVersion != nil {
		version = *appVersion
	}
	_, err := writer.ExecContext(ctx,
		`UPDATE device SET last_time=GREATEST(COALESCE(last_time,?),?),
		 battery=COALESCE(?,battery), app_version=COALESCE(?,app_version),
		 last_online_at=GREATEST(COALESCE(last_online_at,?),?)
		 WHERE device_id=? AND health_epoch=?
		 AND ? >= COALESCE(last_online_at,?)`,
		seenAt, seenAt, b, version, seenAt, seenAt, deviceID, bindingEpoch, seenAt, seenAt)
	return err
}
