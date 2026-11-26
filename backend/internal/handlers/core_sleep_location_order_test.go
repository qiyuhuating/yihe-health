package handlers

import (
	"context"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestContinuousSleepFieldsKeepObservationOrder(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	sleepCount := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.countSleepAlertDevices)
	report := func(at string, fields map[string]any) {
		fields["deviceNo"], fields["secret"], fields["beTime"] = "WATCH001", testSecret, "2026-10-20 "+at
		if out := api(t, h.deviceReport, fields); out["returnCode"] != "1111" {
			t.Fatal(out)
		}
	}
	check := func(wantSleep int, wantState string) {
		t.Helper()
		var sleep int
		var state string
		if err := pool.QueryRowContext(context.Background(), "SELECT sleep,sleep_sate FROM watch_day_info WHERE device_id=1").Scan(&sleep, &state); err != nil {
			t.Fatal(err)
		}
		if sleep != wantSleep || state != wantState {
			t.Errorf("sleep=%d state=%s; want sleep=%d state=%s", sleep, state, wantSleep, wantState)
		}
	}
	report("10:00:00", map[string]any{"sleep": 8, "sleepState": "0"})
	report("09:00:00", map[string]any{"sleep": 2, "sleepState": "1"})
	check(8, "0")
	report("11:00:00", map[string]any{"sleepState": "1"})
	report("10:30:00", map[string]any{"sleep": 7, "sleepState": "0"})
	check(7, "1")
	report("11:30:00", map[string]any{"sleep": 0, "sleepState": "0"})
	check(0, "0")
	if rows := api(t, sleepCount, map[string]any{})["sleep"]; rows != float64(0) {
		t.Fatalf("older sleep state created a current alert: %v", rows)
	}
}

func TestContinuousRebindLocationSnapshotUsesCurrentOwner(t *testing.T) {
	h := dayOwnershipSetup(t)
	heartbeat := api(t, h.deviceHeartbeat, map[string]any{"deviceNo": "WATCH001", "secret": testSecret})
	bindingVersion := heartbeat["bindingVersion"]
	// The previous owner's later observation must not order the new owner's snapshot.
	mustExec(t, h.DB, "INSERT INTO device_location(device_id,old_id,lng,lat,be_time) VALUES(1,1,'120','30','2026-10-20 10:00:00')")
	for _, p := range []struct{ at, lng, lat string }{{"09:00:00", "110", "20"}, {"08:00:00", "111", "21"}} {
		api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "lng": p.lng, "lat": p.lat, "beTime": "2026-10-20 " + p.at})
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "alarm": "1", "bindingVersion": bindingVersion, "beTime": "2026-10-20 09:30:00"})
	var lng, lat *string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT last_lng,last_lat FROM device WHERE device_id=1").Scan(&lng, &lat); err != nil {
		t.Fatal(err)
	}
	lng, lat, err := openCoordinates(h.Box, lng, lat)
	if err != nil || lng == nil || lat == nil || *lng != "110" || *lat != "20" {
		t.Fatalf("current-owner history lost or replaced by older owner/report: lng=%v lat=%v err=%v", lng, lat, err)
	}
	if err := h.DB.QueryRowContext(context.Background(), "SELECT lng,lat FROM alarm_precess WHERE old_id=2 ORDER BY id DESC LIMIT 1").Scan(&lng, &lat); err != nil {
		t.Fatal(err)
	}
	if lng != nil || lat != nil {
		t.Fatalf("alarm borrowed a historical point from either owner: lng=%v lat=%v", lng, lat)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "alarm": "1", "bindingVersion": bindingVersion, "lng": "112", "lat": "22", "beTime": "2026-10-20 09:40:00"})
	if err := h.DB.QueryRowContext(context.Background(), "SELECT lng,lat FROM alarm_precess WHERE old_id=2 ORDER BY id DESC LIMIT 1").Scan(&lng, &lat); err != nil {
		t.Fatal(err)
	}
	lng, lat, err = openCoordinates(h.Box, lng, lat)
	if err != nil || lng == nil || lat == nil || *lng != "112" || *lat != "22" {
		t.Fatalf("explicit new-owner alarm point lost: lng=%v lat=%v err=%v", lng, lat, err)
	}
}
