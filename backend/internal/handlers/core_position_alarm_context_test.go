package handlers

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
	"yihe-care/internal/telemetryrepo"
)

func TestContinuousPositionKeepsAlarmAndLocationSnapshotsSeparate(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	seedDevice(t, h.DB)
	mustExec(t, h.DB, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001')")
	mustExec(t, h.DB, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "alarm": "fall", "lng": "120", "lat": "30", "beTime": "2026-10-20 10:00:00"})
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "lng": "121", "lat": "31", "beTime": "2026-10-20 11:00:00"})
	reads := []struct {
		name string
		read func() map[string]any
	}{
		{"admin list", func() map[string]any {
			return api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})
		}},
		{"admin detail", func() map[string]any {
			return api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelectFamily), map[string]any{"oldId": 1})
		}},
		{"family detail", func() map[string]any {
			return api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}, h.positionSelectFamily), map[string]any{"oldId": 1})
		}},
	}
	for _, reader := range reads {
		row := reader.read()["positionDtos"].([]any)[0].(map[string]any)
		if row["beTime"] != "2026-10-20 11:00:00" || row["lng"] != "121" || row["lat"] != "31" || row["alarmTime"] != "2026-10-20 10:00:00" || row["alarmLng"] != "120" || row["alarmLat"] != "30" {
			t.Errorf("%s mixed incident and latest position: %v", reader.name, row)
		}
	}
	mustExec(t, h.DB, "INSERT INTO family(family_id,family_phone) VALUES(2,'13800000002')")
	mustExec(t, h.DB, "UPDATE oldperson SET family_id=2 WHERE old_id=1")
	former := withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}, h.positionSelectFamily)
	current := withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 1}, h.positionSelectFamily)
	if got := callAPI(former, map[string]any{"oldId": 1}); got.Code != 403 {
		t.Fatalf("family retained location after reassignment: %d %s", got.Code, got.Body.String())
	}
	if got := callAPI(current, map[string]any{"oldId": 1}); got.Code != 200 {
		t.Fatalf("current family denied its resident location: %d %s", got.Code, got.Body.String())
	}
}

func TestContinuousSOSWithoutCoordinatesDoesNotBorrowLatestLocation(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	seedDevice(t, h.DB)
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "lng": "121", "lat": "31", "beTime": "2026-10-20 11:00:00"})
	out := api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "context-sos-no-coordinates"})
	if out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	var eventTime time.Time
	if err := h.DB.QueryRowContext(context.Background(), "SELECT be_time FROM alarm_precess WHERE old_id=1 ORDER BY id DESC LIMIT 1").Scan(&eventTime); err != nil {
		t.Fatal(err)
	}
	row := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if row["status"] != "1" || row["alarmTime"] != eventTime.Format(tsLayout) || row["alarmLng"] != nil || row["alarmLat"] != nil || row["lng"] != "121" || row["lat"] != "31" || row["beTime"] != "2026-10-20 11:00:00" {
		t.Errorf("SOS context invented coordinates or lost its own timestamp: %v", row)
	}
	bad := callAPI(h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "context-sos-unpaired", "lng": "120"})
	if bad.Code != 400 {
		t.Errorf("partial coordinates were combined with cached latitude: HTTP %d %s", bad.Code, bad.Body.String())
	}
	var locations, incidents int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM device_location WHERE old_id=1").Scan(&locations); err != nil || locations != 1 {
		t.Errorf("SOS without a complete fresh point wrote location history: count=%d err=%v", locations, err)
	}
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM care_incident WHERE old_id=1").Scan(&incidents); err != nil || incidents != 1 {
		t.Errorf("malformed coordinates created an extra incident: count=%d err=%v", incidents, err)
	}
	var kind string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT kind FROM care_incident WHERE source_alarm_id=(SELECT max(id) FROM alarm_precess WHERE old_id=1)").Scan(&kind); err != nil || kind != "sos" {
		t.Fatalf("fixture must represent a real SOS, not a fall classification: kind=%s err=%v", kind, err)
	}
}

func TestPositionAlertSummaryUsesLatestAlarmPerResident(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	mustExec(t, h.DB, "INSERT INTO oldperson(old_id,name) VALUES(1,'甲'),(2,'乙'),(3,'丙')")
	mustExec(t, h.DB, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'SUMMARY1','test',1),(2,'SUMMARY2','test',2)")
	ctx := context.Background()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	for _, alarm := range []telemetryrepo.Alarm{
		{ResidentID: 1, DeviceID: 1, ObservedAt: at},
		{ResidentID: 1, DeviceID: 1, ObservedAt: at},
		{ResidentID: 2, DeviceID: 2, ObservedAt: at.Add(-time.Hour)},
		{ResidentID: 2, DeviceID: 2, ObservedAt: at},
	} {
		if _, err := telemetryrepo.InsertAlarm(ctx, tx, alarm); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if resolved, err := telemetryrepo.ResolveLatestLocationAlert(ctx, h.DB, 1); err != nil || !resolved {
		t.Fatalf("resolve latest: resolved=%v err=%v", resolved, err)
	}
	got := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionAlertSummary), map[string]any{})
	if got["totalResidents"] != float64(3) || got["activeAlerts"] != float64(1) {
		t.Fatalf("unexpected summary: %v", got)
	}
}
