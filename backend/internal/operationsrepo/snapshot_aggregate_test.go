package operationsrepo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

// Previous independent COUNT subqueries provide a reference for the grouped
// aggregates, including SQL's empty-table and NULL handling.
const legacySnapshotSQL = `SELECT
	h.heart_rate_count,h.blood_pressure_count,h.sleep_count,
	p.total_residents,p.active_location_alerts,
	(SELECT COUNT(*) FROM care_incident WHERE state<>'verified'),
	(SELECT COUNT(*) FROM care_incident WHERE state<>'verified' AND due_at < ?),
	(SELECT COUNT(*) FROM care_notification_outbox WHERE status='pending'),
	(SELECT COUNT(*) FROM care_notification_outbox WHERE status='failed'),
	(SELECT COUNT(*) FROM care_notification_outbox WHERE status='sending' AND last_attempt_at < ?),
	(SELECT COUNT(*) FROM device WHERE status<>0),
	(SELECT COUNT(*) FROM device WHERE status<>0 AND (last_time IS NULL OR last_time < ?)),
	(SELECT COUNT(*) FROM device WHERE status<>0 AND battery IS NOT NULL AND battery>=0 AND battery<20),
	(SELECT COUNT(*) FROM device WHERE status<>0 AND old_id IS NULL)
	FROM (` + healthAlertCountsSQL + `) h
	CROSS JOIN (` + positionAlertSummarySQL + `) p`

func oldSnapshot(ctx context.Context, q db.RowQuerier, now time.Time) (Snapshot, error) {
	var out Snapshot
	err := q.QueryRowContext(ctx, legacySnapshotSQL, now, now.Add(-2*time.Minute), now.Add(-2*time.Minute)).Scan(
		&out.Health.HeartRate, &out.Health.BloodPressure, &out.Health.Sleep,
		&out.TotalResidents, &out.ActiveLocationAlerts,
		&out.ActiveCareIncidents, &out.OverdueCareIncidents,
		&out.PendingNotifications, &out.FailedNotifications, &out.StaleNotifications,
		&out.ActiveDevices, &out.OfflineDevices, &out.LowBatteryDevices, &out.UnboundDevices,
	)
	out.CapturedAt = now.UTC()
	return out, err
}

func TestSnapshotGroupedCountersMatchLegacyMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	check := func() {
		t.Helper()
		got, err := ReadSnapshot(ctx, pool, now)
		if err != nil {
			t.Fatal(err)
		}
		want, err := oldSnapshot(ctx, pool, now)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("grouped snapshot=%+v, legacy=%+v, err=%v", got, want, err)
		}
	}
	check() // Empty tables must yield numeric zeros, never NULL.
	for _, statement := range []string{
		"INSERT INTO oldperson(old_id,name) VALUES(1,'A'),(2,'B')",
		"INSERT INTO device(device_id,device_no,secret,status,old_id,last_time,battery) VALUES " +
			"(1,'AGG001','secret',1,1,'2026-11-23 11:59:30',0)," +
			"(2,'AGG002','secret',1,NULL,'2026-11-23 11:50:00',15)," +
			"(3,'AGG003','secret',0,2,NULL,19)",
		"INSERT INTO care_incident(id,old_id,device_id,state,due_at) VALUES " +
			"(1,1,1,'received','2026-11-23 11:00:00')," +
			"(2,1,1,'verified','2026-11-23 11:00:00')," +
			"(3,2,3,'in_progress','2026-11-23 13:00:00')",
		"INSERT INTO care_notification_outbox(id,incident_id,channel,recipient,recipient_key,status,last_attempt_at) VALUES " +
			"(1,1,'sms','a',REPEAT('a',64),'pending',NULL)," +
			"(2,1,'sms','b',REPEAT('b',64),'failed',NULL)," +
			"(3,1,'sms','c',REPEAT('c',64),'sending','2026-11-23 11:00:00')," +
			"(4,1,'sms','d',REPEAT('d',64),'sending','2026-11-23 11:59:30')," +
			"(5,1,'sms','e',REPEAT('e',64),'delivered',NULL)",
	} {
		if _, err := pool.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	check()
}
