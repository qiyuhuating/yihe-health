package handlers

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestPR50AlarmContextRetainsDeclaredMetadataAndKind(t *testing.T) {
	h := extraAuditHandler(t)
	seedDevice(t, h.DB)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	mustExec(t, h.DB, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001')")
	mustExec(t, h.DB, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	api(t, h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret, "alarm": "fall",
		"beTime": "2026-10-20 10:00:00", "lng": "120", "lat": "30",
		"positionSource": "gps", "positionSampledAt": "2026-10-20T02:00:00Z", "positionAccuracy": 12.5,
	})
	row := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if row["alarmKind"] != "fall" || row["alarmSource"] != "gps" || row["alarmSampledAt"] != "2026-10-20T02:00:00Z" || row["alarmAccuracy"] != 12.5 || row["alarmValidity"] != "reported" {
		t.Errorf("declared alarm metadata was lost or invented: %v", row)
	}
	for _, principal := range []session.Principal{{Role: auth.RoleAdmin, ID: 11}, {Role: auth.RoleFamily, ID: 1, OldID: 1}} {
		detail := api(t, withHealthPrincipal(h, principal, h.positionSelectFamily), map[string]any{"oldId": 1})["positionDtos"].([]any)[0].(map[string]any)
		if detail["alarmKind"] != "fall" || detail["alarmSource"] != "gps" || detail["alarmSampledAt"] != "2026-10-20T02:00:00Z" || detail["alarmAccuracy"] != 12.5 {
			t.Errorf("scoped detail lost the event metadata: %v", detail)
		}
	}
	var raw string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT position_metadata FROM alarm_precess ORDER BY id LIMIT 1").Scan(&raw); err != nil || !strings.HasPrefix(raw, securestore.Prefix) {
		t.Fatalf("location metadata must use existing field encryption: raw=%q err=%v", raw, err)
	}
	api(t, h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"beTime": "2026-10-20 11:00:00", "lng": "121", "lat": "31",
		"positionSource": "network", "positionSampledAt": "2026-10-20T03:00:00Z", "positionAccuracy": 80,
	})
	row = api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if row["alarmKind"] != "fall" || row["alarmSource"] != "gps" || row["alarmSampledAt"] != "2026-10-20T02:00:00Z" || row["alarmAccuracy"] != 12.5 || row["positionSource"] != "network" || row["positionSampledAt"] != "2026-10-20T03:00:00Z" || row["positionAccuracy"] != float64(80) {
		t.Errorf("later positioning changed the alarm context or lost its own metadata: %v", row)
	}
	api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "pr50-no-position-metadata"})
	row = api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if row["alarmKind"] != "sos" || row["alarmValidity"] != "missing" || row["alarmSource"] != nil || row["alarmSampledAt"] != nil || row["alarmAccuracy"] != nil || row["alarmLng"] != nil || row["alarmLat"] != nil {
		t.Errorf("SOS without location must not inherit context from positioning: %v", row)
	}
	retry := api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "pr50-no-position-metadata", "lng": "120", "lat": "30", "positionSource": "gps", "positionSampledAt": "2026-10-20T02:00:00Z", "positionAccuracy": 1})
	row = api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if retry["duplicate"] != true || row["alarmValidity"] != "missing" || row["alarmSource"] != nil || row["alarmAccuracy"] != nil || row["alarmLng"] != nil || row["positionSource"] != "network" {
		t.Errorf("retry rewrote the first event metadata or independent latest positioning: retry=%v row=%v", retry, row)
	}
}

func TestPR50InvalidPositionMetadataDoesNotWriteEvents(t *testing.T) {
	h := extraAuditHandler(t)
	seedDevice(t, h.DB)
	for i, tc := range []struct {
		key   string
		value any
	}{{"positionSource", "sos"}, {"positionSampledAt", "2026-10-20 02:00:00"}, {"positionAccuracy", 0}, {"positionAccuracy", -1}, {"positionAccuracy", "5"}} {
		body := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "pr50-invalid-" + strconv.Itoa(i), "alarm": "fall", "lng": "120", "lat": "30", "beTime": "2026-10-20 10:00:00", tc.key: tc.value}
		if out := callAPI(h.deviceReport, body); out.Code != 400 || !strings.Contains(out.Body.String(), tc.key) {
			t.Errorf("report accepted %s=%v: HTTP %d %s", tc.key, tc.value, out.Code, out.Body.String())
		}
		if out := callAPI(h.deviceSos, body); out.Code != 400 || !strings.Contains(out.Body.String(), tc.key) {
			t.Errorf("SOS accepted %s=%v: HTTP %d %s", tc.key, tc.value, out.Code, out.Body.String())
		}
	}
	for _, table := range []string{"alarm_precess", "device_location", "care_incident"} {
		var count int
		if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Errorf("invalid metadata wrote %s: count=%d err=%v", table, count, err)
		}
	}
}

func TestPR50HistoricalMetadataAndGenericAlarmRemainUnknown(t *testing.T) {
	h := extraAuditHandler(t)
	seedDevice(t, h.DB)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	mustExec(t, h.DB, "INSERT INTO alarm_precess(old_id,device_id,status,lng,lat,be_time) VALUES(1,1,'1','120','30','2026-10-20 10:00:00')")
	mustExec(t, h.DB, "INSERT INTO device_location(old_id,device_id,lng,lat,source,be_time) VALUES(1,1,'120','30','gps','2026-10-20 10:00:00')")
	row := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if row["alarmKind"] != "unknown" || row["alarmValidity"] != "unknown" || row["alarmSource"] != nil || row["alarmSampledAt"] != nil || row["alarmAccuracy"] != nil || row["positionSource"] != nil || row["positionValidity"] != "unknown" {
		t.Errorf("historical coordinates/shared flag were turned into fabricated context: %v", row)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "alarm": "1", "lng": "121", "lat": "31", "beTime": "2026-10-20 11:00:00"})
	row = api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
	if row["alarmKind"] != "unknown" || row["alarmValidity"] != "reported" || row["alarmSource"] != nil || row["alarmSampledAt"] != nil || row["alarmAccuracy"] != nil {
		t.Errorf("generic shared alarm guessed a type, source, sampling clock or accuracy: %v", row)
	}
}
