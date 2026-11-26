package handlers

import (
	"context"
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestTargetedVitalAlertResolutionIsReplaySafe(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	h := testHandler(t, pool)
	principal := session.Principal{Role: auth.RoleAdmin, ID: 71}
	mustExec(t, pool, `INSERT INTO ratepre(id,device_id,old_id,hr,bp,rate_state,pre_state,create_time)
		VALUES(1,1,1,150,180,'1','1','2026-10-20 10:00:00'),
		      (2,1,1,160,190,'1','1','2026-10-20 11:00:00')`)

	direct := withHealthPrincipal(h, principal, h.handleResolveVitalAlert)
	record := map[string]any{"recordId": 1, "kind": "rate"}
	if got := callAPI(direct, record); got.Code != http.StatusOK {
		t.Fatalf("first vital resolution: HTTP %d %s", got.Code, got.Body.String())
	}
	var handledAt string
	var state, otherState string
	err := pool.QueryRowContext(context.Background(), `SELECT DATE_FORMAT(rate_handled_at,'%Y-%m-%d %H:%i:%s.%f'),
		rate_state,pre_state FROM ratepre WHERE id=1`).Scan(&handledAt, &state, &otherState)
	if err != nil || handledAt == "" || state != "0" || otherState != "1" {
		t.Fatalf("first vital resolution failed: handled=%s state=%s/%s err=%v", handledAt, state, otherState, err)
	}
	if got := callAPI(direct, record); got.Code == http.StatusOK {
		t.Fatalf("replayed vital resolution succeeded: %s", got.Body.String())
	}
	var replayAt string
	if err := pool.QueryRowContext(context.Background(),
		"SELECT DATE_FORMAT(rate_handled_at,'%Y-%m-%d %H:%i:%s.%f') FROM ratepre WHERE id=1").Scan(&replayAt); err != nil || replayAt != handledAt {
		t.Fatalf("replay rewrote handled timestamp: before=%s after=%s err=%v", handledAt, replayAt, err)
	}

	device := withHealthPrincipal(h, principal, h.deviceResolveAlert)
	deviceRecord := map[string]any{"deviceId": 1, "recordId": 2, "kind": "rate"}
	if got := callAPI(device, deviceRecord); got.Code != http.StatusOK {
		t.Fatalf("first device resolution: HTTP %d %s", got.Code, got.Body.String())
	}
	var deviceAt string
	if err := pool.QueryRowContext(context.Background(),
		"SELECT DATE_FORMAT(rate_handled_at,'%Y-%m-%d %H:%i:%s.%f') FROM ratepre WHERE id=2").Scan(&deviceAt); err != nil || deviceAt == "" {
		t.Fatalf("device resolution timestamp: %s err=%v", deviceAt, err)
	}
	var audits int
	if err := pool.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM care_access_audit WHERE admin_id=71 AND action='device_alert_resolve'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("first device resolution audit: count=%d err=%v", audits, err)
	}
	if got := callAPI(device, deviceRecord); got.Code == http.StatusOK {
		t.Fatalf("replayed device resolution succeeded: %s", got.Body.String())
	}
	var replayDeviceAt string
	if err := pool.QueryRowContext(context.Background(),
		"SELECT DATE_FORMAT(rate_handled_at,'%Y-%m-%d %H:%i:%s.%f') FROM ratepre WHERE id=2").Scan(&replayDeviceAt); err != nil || replayDeviceAt != deviceAt {
		t.Fatalf("device replay rewrote timestamp: before=%s after=%s err=%v", deviceAt, replayDeviceAt, err)
	}
	if err := pool.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM care_access_audit WHERE admin_id=71 AND action='device_alert_resolve'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("device replay duplicated audit: count=%d err=%v", audits, err)
	}
}
