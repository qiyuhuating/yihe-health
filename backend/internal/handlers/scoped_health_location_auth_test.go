package handlers

import (
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestScopedHealthAndLocationReadsRecheckLiveDutyAndFamilyBinding(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	seedCareAdminDuty(t, pool, 73, "auditor")
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001'),(2,'13800000002')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)

	healthQuery := map[string]any{
		"oldId": 1, "source": "watch", "sourceDevice": "WATCH001",
		"metric": "heart_rate", "semantics": "instant",
		"from": "2026-10-20T00:00:00Z", "to": "2026-10-21T00:00:00Z",
	}
	reads := []struct {
		name string
		fn   http.HandlerFunc
		body map[string]any
	}{
		{"health_statistics", h.healthStatistics, healthQuery},
		{"resident_location", h.positionSelectFamily, map[string]any{"oldId": 1}},
	}
	check := func(p session.Principal, want int) {
		t.Helper()
		for _, read := range reads {
			got := callAPI(withHealthPrincipal(h, p, read.fn), read.body)
			if got.Code != want {
				t.Fatalf("%s actor=%s/%d: HTTP %d want %d: %s",
					read.name, p.Role, p.ID, got.Code, want, got.Body.String())
			}
		}
	}

	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	familyOne := session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}
	familyTwo := session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 1}
	check(supervisor, http.StatusOK)
	check(session.Principal{Role: auth.RoleAdmin, ID: 72}, http.StatusForbidden)
	check(session.Principal{Role: auth.RoleAdmin, ID: 73}, http.StatusForbidden)
	check(familyOne, http.StatusOK)
	check(familyTwo, http.StatusForbidden)

	latest := withHealthPrincipal(h, supervisor, h.listLatestVitals)
	if got := callAPI(latest, map[string]any{"page": 1, "pageSize": 20}); got.Code != http.StatusOK {
		t.Fatalf("supervisor global vitals query failed: HTTP %d %s", got.Code, got.Body.String())
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	check(supervisor, http.StatusForbidden)
	if got := callAPI(latest, map[string]any{"page": 1, "pageSize": 20}); got.Code != http.StatusForbidden {
		t.Fatalf("revoked supervisor accessed global vitals: HTTP %d %s", got.Code, got.Body.String())
	}

	mustExec(t, pool, "UPDATE oldperson SET family_id=2 WHERE old_id=1")
	check(familyOne, http.StatusForbidden)
	check(familyTwo, http.StatusOK)
}

func TestAssignDutyHandlerUsesWorkflowAuthorization(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	seedCareAdminDuty(t, pool, 73, "auditor")
	h := testHandler(t, pool)
	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	caregiver := session.Principal{Role: auth.RoleAdmin, ID: 72}
	change := func(actor session.Principal, target string, duty string) *http.Response {
		t.Helper()
		return callAPI(withHealthPrincipal(h, actor, h.careAssignDuty), map[string]any{
			"adminId": target, "duty": duty,
		}).Result()
	}
	if got := change(caregiver, "73", "caregiver"); got.StatusCode != http.StatusForbidden {
		t.Fatalf("caregiver assigned staff: HTTP %d", got.StatusCode)
	}
	if got := change(supervisor, "71", "caregiver"); got.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-demotion contract changed: HTTP %d", got.StatusCode)
	}
	if got := change(supervisor, "73", "caregiver"); got.StatusCode != http.StatusOK {
		t.Fatalf("supervisor assignment failed: HTTP %d", got.StatusCode)
	}
	var duty string
	if err := pool.QueryRowContext(t.Context(), "SELECT duty FROM care_staff_permission WHERE admin_id=73").Scan(&duty); err != nil || duty != "caregiver" {
		t.Fatalf("staff change not committed: duty=%q err=%v", duty, err)
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	if got := change(supervisor, "73", "auditor"); got.StatusCode != http.StatusForbidden {
		t.Fatalf("revoked supervisor assigned staff: HTTP %d", got.StatusCode)
	}
	if err := pool.QueryRowContext(t.Context(), "SELECT duty FROM care_staff_permission WHERE admin_id=73").Scan(&duty); err != nil || duty != "caregiver" {
		t.Fatalf("denied assignment changed duty: duty=%q err=%v", duty, err)
	}
}
