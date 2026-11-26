package handlers

import (
	"context"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func dayOwnershipSetup(t *testing.T) *Handler {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	unbind := withHealthPrincipal(h, admin, h.deviceUnbind)
	createCode := withHealthPrincipal(h, admin, h.deviceCreateBindCode)
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone) VALUES(2,'乙家属','13800000002')")
	mustExec(t, pool, "UPDATE oldperson SET device_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,family_id) VALUES(2,'乙老人',2)")
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": 5000, "sleep": 8, "sleepState": "1", "beTime": "2026-10-20 08:00:00"})
	api(t, unbind, map[string]any{"deviceId": 1})
	code := api(t, createCode, map[string]any{"oldId": 2, "expireDays": 3})
	out := api(t, h.deviceBind, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "code": code["code"]})
	if out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	return h
}

func TestContinuousRebindCannotReadPreviousDailyData(t *testing.T) {
	h := dayOwnershipSetup(t)
	for _, handler := range []struct {
		name, key string
		read      func() map[string]any
	}{
		{"family latest", "activityRecords", func() map[string]any {
			return api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 2}, h.listActivityHistory), map[string]any{"oldId": 2, "expireDays": 3})
		}},
		{"family history", "activityRecords", func() map[string]any {
			return api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 2}, h.getActivityChart), map[string]any{"oldId": 2, "expireDays": 3})
		}},
		{"admin latest", "activityRecords", func() map[string]any {
			return api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.listLatestActivity), map[string]any{"name": "乙老人"})
		}},
	} {
		out := handler.read()
		if rows := out[handler.key].([]any); len(rows) != 0 {
			t.Errorf("%s exposed previous elder's data: %v", handler.name, out)
		}
	}
	if out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.countSleepAlertDevices), map[string]any{}); out["sleep"] != float64(0) {
		t.Errorf("previous binding's sleep alarm counted for current device: %v", out)
	}
}

func TestContinuousRebindDailySnapshotIsSeparate(t *testing.T) {
	h := dayOwnershipSetup(t)
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	unbind := withHealthPrincipal(h, admin, h.deviceUnbind)
	createCode := withHealthPrincipal(h, admin, h.deviceCreateBindCode)
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": 10, "beTime": "2026-10-20 09:00:00"})
	out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 2}, h.listActivityHistory), map[string]any{"oldId": 2, "expireDays": 3})
	row := out["activityRecords"].([]any)[0].(map[string]any)
	if row["step"] != float64(10) || row["sleep"] != nil || row["sleepAlertState"] != nil {
		t.Fatalf("mixed daily fields across bindings: %v", row)
	}
	var oldSteps, oldSleep, owner int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT step,sleep,old_id FROM watch_day_info_archive WHERE device_id=1 ORDER BY id DESC LIMIT 1").Scan(&oldSteps, &oldSleep, &owner); err != nil {
		t.Fatal(err)
	}
	if oldSteps != 5000 || oldSleep != 8 || owner != 1 {
		t.Fatalf("previous daily snapshot not preserved: steps=%d sleep=%d owner=%d", oldSteps, oldSleep, owner)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "sleep": 6, "sleepState": "0", "beTime": "2026-10-20 07:30:00"})
	var currentSleep int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT sleep FROM watch_day_info WHERE device_id=1").Scan(&currentSleep); err != nil || currentSleep != 6 {
		t.Fatalf("new binding inherited previous sleep clock: sleep=%d err=%v", currentSleep, err)
	}
	// Returning to the first elder exposes only that elder's archived day,
	// then replaces it with the new binding's independent snapshot.
	api(t, unbind, map[string]any{"deviceId": 1})
	code := api(t, createCode, map[string]any{"oldId": 1, "expireDays": 3})
	if bound := api(t, h.deviceBind, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "code": code["code"]}); bound["returnCode"] != "1111" {
		t.Fatal(bound)
	}
	old := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.listActivityHistory), map[string]any{"oldId": 1})["activityRecords"].([]any)
	if len(old) != 1 || old[0].(map[string]any)["step"] != float64(5000) {
		t.Fatalf("original elder's history lost: %v", old)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": 20, "beTime": "2026-10-20 08:30:00"})
	current := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.getActivityChart), map[string]any{"oldId": 1})["activityRecords"].([]any)
	if len(current) != 1 || current[0].(map[string]any)["step"] != float64(20) || current[0].(map[string]any)["sleep"] != nil {
		t.Fatalf("repeated binding did not start a distinct daily snapshot: %v", current)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "sleep": 4, "beTime": "2026-10-20 07:00:00"})
	if err := h.DB.QueryRowContext(context.Background(), "SELECT sleep FROM watch_day_info WHERE device_id=1").Scan(&currentSleep); err != nil || currentSleep != 4 {
		t.Fatalf("returning binding inherited archived sleep clock: sleep=%d err=%v", currentSleep, err)
	}
	var count int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM watch_day_info_archive WHERE device_id=1").Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected both older snapshots preserved: count=%d err=%v", count, err)
	}
}
