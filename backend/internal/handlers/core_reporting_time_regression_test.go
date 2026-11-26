package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestContinuousReportFutureClock(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.FixedZone("reporting", 8*3600))
	for _, tc := range []struct {
		at    string
		valid bool
	}{
		{"", true},
		{now.Add(-24 * time.Hour).Format(tsLayout), true},
		{now.Add(5 * time.Minute).Format(tsLayout), true},
		{now.Add(6 * time.Minute).Format(tsLayout), false},
		{"2099-01-01 00:00:00", false},
	} {
		w := httptest.NewRecorder()
		_, valid := parseDeviceTelemetryPayload(w, map[string]any{"beTime": tc.at, "step": "100"}, now)
		if valid != tc.valid || (!tc.valid && w.Code != http.StatusBadRequest) {
			t.Errorf("beTime=%q valid=%v HTTP=%d wantValid=%v", tc.at, valid, w.Code, tc.valid)
		}
	}
}

func TestContinuousSleepCountUsesLatestDay(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	sleepCount := withHealthPrincipal(h, admin, h.countSleepAlertDevices)
	healthCount := withHealthPrincipal(h, admin, h.countHealthAlertDevices)
	for _, v := range []struct{ day, state string }{{"2026-10-18 08:00:00", "1"}, {"2026-10-20 08:00:00", "0"}, {"2026-10-19 08:00:00", "1"}} {
		api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "sleep": 8, "sleepState": v.state, "beTime": v.day})
	}
	out := api(t, sleepCount, map[string]any{})
	if out["sleep"] != float64(0) {
		t.Errorf("old abnormal sleep still counted: %v", out)
	}
	if total := api(t, healthCount, map[string]any{}); total["sleep"] != float64(0) {
		t.Errorf("overview retained recovered sleep alert: %v", total)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "sleepState": "1", "beTime": "2026-10-20 09:00:00"})
	if list := api(t, sleepCount, map[string]any{})["sleep"]; list != float64(1) {
		t.Fatalf("latest abnormal sleep not counted: %v", list)
	}
}
