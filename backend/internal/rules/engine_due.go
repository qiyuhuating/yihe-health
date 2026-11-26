package rules

import (
	"context"
	"database/sql"
	"time"
)

func scheduleRuleTask(ctx context.Context, tx *sql.Tx, device ruleDevice, due any) error {
	_, err := tx.ExecContext(ctx, "UPDATE rule_task SET due_at=? WHERE device_id=? AND epoch=?", due, device.id, device.epoch)
	return err
}

// Compute the next local-clock boundary while the current UTC offset holds.
// At a timezone transition, re-evaluate and compute the boundary again. This
// handles skipped and repeated local hours without time.Date ambiguity.
func nextNightBoundary(p Policy, now time.Time) time.Time {
	loc, err := ruleLocation(p.Timezone)
	if err != nil {
		return now.Add(5 * time.Second)
	}
	local := now.In(loc)
	seconds := local.Hour()*3600 + local.Minute()*60 + local.Second()
	var next time.Time
	for _, boundary := range [...]int{p.NightBegin, p.NightEnd} {
		delta := boundary - seconds
		if delta <= 0 {
			delta += 86400
		}
		at := now.Truncate(time.Second).Add(time.Duration(delta) * time.Second)
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	_, end := local.ZoneBounds()
	if !end.IsZero() && end.After(now) && end.Before(next) {
		next = end
	}
	return next.UTC()
}

func nextRuleTask(ctx context.Context, tx *sql.Tx, device ruleDevice, p Policy, in Input, nightIncident bool) (time.Time, error) {
	now := in.Now
	next := nextNightBoundary(p, now)
	// The offline condition is strictly greater than the configured timeout.
	// Schedule the first representable MySQL timestamp after that boundary.
	// Once breached, the next report/policy write wakes the task; there is no
	// need to rewrite the same observation every minute.
	if !in.LastReport.IsZero() {
		at := in.LastReport.Add(time.Duration(p.OfflineSeconds)*time.Second + time.Microsecond)
		if at.After(now) && at.Before(next) {
			next = at
		}
	}
	// Recent observations may become baseline-eligible or their rolling
	// activity window may become valid without another write. An unresolved
	// night incident may expose an older incident when it is verified.
	if in.Heart != nil || in.Blood != nil || in.activityObserved || nightIncident {
		at := now.Add(5 * time.Second)
		if at.Before(next) {
			next = at
		}
	}
	// Future-dated samples do not enter the latest window until their
	// timestamp. Use the same indexed owner/device/epoch/metric predicates.
	// No extra query is needed if the next check is already within 5 seconds.
	if next.After(now.Add(5 * time.Second)) {
		var future *int64
		err := tx.QueryRowContext(ctx, `SELECT MIN(sample_start) FROM health_sample
 WHERE old_id=? AND device_id=? AND epoch=? AND metric IN ('heart_rate','blood_pressure_systolic','steps')
 AND NOT deleted AND sample_start>? AND sample_start<=?`, device.oldID, device.id, device.epoch, now.UnixMicro(), next.UnixMicro()).Scan(&future)
		if err != nil {
			return time.Time{}, err
		}
		if future != nil {
			at := time.UnixMicro(*future).UTC()
			if at.Before(next) {
				next = at
			}
		}
	}
	return next, nil
}
