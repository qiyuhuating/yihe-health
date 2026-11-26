package operationsrepo

import (
	"context"
	"time"

	"yihe-care/internal/db"
)

const healthAlertCountsSQL = `SELECT
	COUNT(CASE WHEN s.heart_rate_alert THEN 1 END) AS heart_rate_count,
	COUNT(CASE WHEN s.blood_pressure_alert THEN 1 END) AS blood_pressure_count,
	COUNT(CASE WHEN s.sleep_alert THEN 1 END) AS sleep_count
	FROM device_health_alert_state s
	INNER JOIN device d ON d.device_id=s.device_id AND d.health_epoch=s.binding_epoch
	WHERE d.status<>0`

const positionAlertSummarySQL = `SELECT
	(SELECT COUNT(*) FROM oldperson) AS total_residents,
	(SELECT COUNT(*) FROM resident_location_alert_state WHERE active=true) AS active_location_alerts`

type HealthAlerts struct {
	HeartRate     int64 `json:"heartRate"`
	BloodPressure int64 `json:"bloodPressure"`
	Sleep         int64 `json:"sleep"`
}

type Snapshot struct {
	Health               HealthAlerts `json:"health"`
	TotalResidents       int64        `json:"totalResidents"`
	ActiveLocationAlerts int64        `json:"activeLocationAlerts"`
	ActiveCareIncidents  int64        `json:"activeCareIncidents"`
	OverdueCareIncidents int64        `json:"overdueCareIncidents"`
	PendingNotifications int64        `json:"pendingNotifications"`
	FailedNotifications  int64        `json:"failedNotifications"`
	StaleNotifications   int64        `json:"staleNotifications"`
	ActiveDevices        int64        `json:"activeDevices"`
	OfflineDevices       int64        `json:"offlineDevices"`
	LowBatteryDevices    int64        `json:"lowBatteryDevices"`
	UnboundDevices       int64        `json:"unboundDevices"`
	CapturedAt           time.Time    `json:"capturedAt"`
}

func ReadHealthAlerts(ctx context.Context, q db.RowQuerier) (HealthAlerts, error) {
	var out HealthAlerts
	err := q.QueryRowContext(ctx, healthAlertCountsSQL).Scan(&out.HeartRate, &out.BloodPressure, &out.Sleep)
	return out, err
}

func ReadPositionAlerts(ctx context.Context, q db.RowQuerier) (total, active int64, err error) {
	err = q.QueryRowContext(ctx, positionAlertSummarySQL).Scan(&total, &active)
	return
}

// ReadSnapshot returns one consistent operational read model in one database round trip.
func ReadSnapshot(ctx context.Context, q db.RowQuerier, now time.Time) (Snapshot, error) {
	var out Snapshot
	// Aggregate each table once instead of issuing nine independent count scans.
	// The filters are unchanged, including null handling and the status gate.
	query := `SELECT
		h.heart_rate_count,h.blood_pressure_count,h.sleep_count,
		p.total_residents,p.active_location_alerts,
		c.active_incidents,c.overdue_incidents,
		n.pending_notifications,n.failed_notifications,n.stale_notifications,
		d.active_devices,d.offline_devices,d.low_battery_devices,d.unbound_devices
		FROM (` + healthAlertCountsSQL + `) h
		CROSS JOIN (` + positionAlertSummarySQL + `) p
		CROSS JOIN (
			SELECT COUNT(*) AS active_incidents,
			COUNT(CASE WHEN due_at < ? THEN 1 END) AS overdue_incidents
			FROM care_incident WHERE state<>'verified'
		) c
		CROSS JOIN (
			SELECT COUNT(CASE WHEN status='pending' THEN 1 END) AS pending_notifications,
			COUNT(CASE WHEN status='failed' THEN 1 END) AS failed_notifications,
			COUNT(CASE WHEN status='sending' AND last_attempt_at < ? THEN 1 END) AS stale_notifications
			FROM care_notification_outbox WHERE status IN ('pending','failed','sending')
		) n
		CROSS JOIN (
			SELECT COUNT(*) AS active_devices,
			COUNT(CASE WHEN last_time IS NULL OR last_time < ? THEN 1 END) AS offline_devices,
			COUNT(CASE WHEN battery IS NOT NULL AND battery>=0 AND battery<20 THEN 1 END) AS low_battery_devices,
			COUNT(CASE WHEN old_id IS NULL THEN 1 END) AS unbound_devices
			FROM device WHERE status<>0
		) d`
	err := q.QueryRowContext(ctx, query, now, now.Add(-2*time.Minute), now.Add(-2*time.Minute)).Scan(
		&out.Health.HeartRate, &out.Health.BloodPressure, &out.Health.Sleep,
		&out.TotalResidents, &out.ActiveLocationAlerts,
		&out.ActiveCareIncidents, &out.OverdueCareIncidents,
		&out.PendingNotifications, &out.FailedNotifications, &out.StaleNotifications,
		&out.ActiveDevices, &out.OfflineDevices, &out.LowBatteryDevices, &out.UnboundDevices,
	)
	out.CapturedAt = now.UTC()
	return out, err
}
