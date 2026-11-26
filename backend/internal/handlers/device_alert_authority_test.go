package handlers

import (
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestDeviceAlertRoutesRequireLiveSupervisor(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	h := testHandler(t, pool)
	cases := []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]any
	}{
		{"counts", h.deviceAlertCounts, map[string]any{}},
		{"detail", h.deviceAlertDetail, map[string]any{"deviceId": "1", "kind": "rate"}},
		{"resolve", h.deviceResolveAlert, map[string]any{"deviceId": "1", "kind": "rate"}},
	}
	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	for _, tc := range cases[:2] {
		got := callAPI(withHealthPrincipal(h, supervisor, tc.handler), tc.body)
		if got.Code != http.StatusOK {
			t.Fatalf("supervisor %s HTTP %d: %s", tc.name, got.Code, got.Body.String())
		}
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	for _, actor := range []int64{71, 72} {
		p := session.Principal{Role: auth.RoleAdmin, ID: actor}
		for _, tc := range cases {
			got := callAPI(withHealthPrincipal(h, p, tc.handler), tc.body)
			if got.Code != http.StatusForbidden {
				t.Fatalf("actor=%d %s HTTP %d: %s", actor, tc.name, got.Code, got.Body.String())
			}
		}
	}
}
