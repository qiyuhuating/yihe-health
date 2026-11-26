package rules

import (
	"context"
	"database/sql"
	"errors"

	"time"
)

func upgradeNightIncident(ctx context.Context, tx *sql.Tx, device ruleDevice, p Policy, now time.Time) error {
	id, oldID := device.id, device.oldID
	var err error
	// Upgrade the existing night SOS/fence incident instead of creating a duplicate.
	if p.Night(now) {
		var incident int64
		var at time.Time
		err = tx.QueryRowContext(ctx, "SELECT id,created_at FROM care_incident WHERE device_id=? AND old_id=? AND kind IN ('sos','fence_exit') AND state<>'verified' ORDER BY id DESC LIMIT 1", id, oldID).Scan(&incident, &at)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && p.Night(at) {
			var count int
			err = tx.QueryRowContext(ctx, "SELECT count(*) FROM care_incident_event WHERE incident_id=? AND action='night_rule'", incident).Scan(&count)
			if err != nil {
				return err
			}
			if count == 0 {
				if _, err = tx.ExecContext(ctx, "UPDATE care_incident SET severity='critical',updated_at=CURRENT_TIMESTAMP(6) WHERE id=?", incident); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, "INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(?,'rule','night_rule','夜间 SOS 或围栏离开，升级现有任务')", incident); err != nil {
					return err
				}
			}
		}
	}
	return nil

}
