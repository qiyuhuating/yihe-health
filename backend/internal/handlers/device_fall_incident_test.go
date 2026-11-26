package handlers

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestFallReportCreatesActionableCareIncidentWithoutDuplicateNotifications(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'测试家属','13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)
	payload := map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"alarm": "fall", "requestId": "fall-event-001",
		"bindingVersion": 1,
		"positionSource": "simulated", "lng": 113.0625, "lat": 28.1975,
	}
	first := api(t, h.deviceReport, payload)
	if first["incidentState"] != "received" || first["returnCode"] != "1111" {
		t.Fatalf("fall was not acknowledged as care incident: %#v", first)
	}
	incidentID := int64(first["incidentId"].(float64))
	ctx := context.Background()
	var alarmID int64
	var kind, state, notification string
	if err := pool.QueryRowContext(ctx, `SELECT i.source_alarm_id,i.kind,i.state,n.status
		FROM care_incident i JOIN care_notification_outbox n ON n.incident_id=i.id
		WHERE i.id=?`, incidentID).Scan(&alarmID, &kind, &state, &notification); err != nil {
		t.Fatal(err)
	}
	if alarmID <= 0 || kind != "fall" || state != "received" || notification != "pending" {
		t.Fatalf("fall bridge: alarm=%d kind=%q state=%q notification=%q", alarmID, kind, state, notification)
	}
	var events, inbox int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_incident_event WHERE incident_id=? AND action='received'", incidentID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("expected one durable received event, got %d", events)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE topic='care' AND source_key=? AND channel='in_app'", fmt.Sprint(incidentID)).Scan(&inbox); err != nil {
		t.Fatal(err)
	}
	if inbox != 1 {
		t.Fatalf("missing family inbox: %d", inbox)
	}

	replayed := api(t, h.deviceReport, payload)
	if replayed["duplicate"] != true || replayed["incidentId"] != first["incidentId"] {
		t.Fatalf("retransmission duplicated incident: first=%#v repeated=%#v", first, replayed)
	}
	var incidents, notifications int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE device_id=1").Scan(&incidents); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_notification_outbox WHERE incident_id=?", incidentID).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if incidents != 1 || notifications != 1 {
		t.Fatalf("duplicate care effects: incidents=%d notifications=%d", incidents, notifications)
	}

	// MySQL binding triggers own health_epoch: manually assigning it is
	// intentionally ignored. A real owner change must increment the epoch.
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	mustExec(t, pool, "UPDATE device SET old_id=1 WHERE device_id=1")
	var recordedEpoch int64
	if err := pool.QueryRowContext(ctx, "SELECT health_epoch FROM device WHERE device_id=1").Scan(&recordedEpoch); err != nil || recordedEpoch != 3 {
		t.Fatalf("owner changes did not increment binding epoch: got=%d err=%v", recordedEpoch, err)
	}
	device, _, _, err := h.hwAuth(ctx, pool, "WATCH001", testSecret, "设备已停用")
	if err != nil || device == nil || device.HealthEpoch != recordedEpoch {
		t.Fatalf("authenticated report read stale binding: device=%+v err=%v", device, err)
	}
	rejected := callAPI(h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"alarm": "fall", "requestId": "fall-after-rebind",
		"bindingVersion": 1,
	})
	if rejected.Code != http.StatusConflict {
		t.Fatalf("stale binding accepted: %d %s", rejected.Code, rejected.Body.String())
	}
}
