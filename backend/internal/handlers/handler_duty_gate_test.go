package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestAdminDutyGateRestrictsLegacyRoutes(t *testing.T) {
	pool := testDatabase(t)
	const adminID int64 = 987654
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role,user_phone) VALUES(?,'GateTest','1','13900009876')", adminID)
	mustExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(?,'caregiver',?)", adminID, adminID)

	store := session.NewStore(time.Hour)
	sess := store.Create(session.Principal{Role: auth.RoleAdmin, ID: adminID})
	if sess == nil {
		t.Fatal("admin session unavailable")
	}
	h := testHandler(t, pool)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	gate := auth.Middleware(store, h.EnforceAdminDuty(next))
	check := func(path string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
		rec := httptest.NewRecorder()
		gate.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("POST %s returned %d, want %d: %s", path, rec.Code, want, rec.Body.String())
		}
	}
	for _, duty := range []string{"caregiver", "auditor"} {
		mustExec(t, pool, "UPDATE care_staff_permission SET duty=? WHERE admin_id=?", duty, adminID)
		check("/api/v1/guardians/delete", http.StatusForbidden)
		check("/api/v1/device/list", http.StatusForbidden)
		check("/api/v1/health/statistics", http.StatusForbidden)
		check("/api/v1/care/transition", http.StatusNoContent)
		check("/api/v1/security/factor", http.StatusNoContent)
	}
	var deniedAuditCount int
	if err := pool.QueryRowContext(
		context.Background(),
		"SELECT count(*) FROM care_sensitive_access_audit WHERE actor_id=? AND resource='/api/v1/legacy-admin-denied'",
		adminID,
	).Scan(&deniedAuditCount); err != nil || deniedAuditCount != 6 {
		t.Fatalf("expected one privacy-safe access audit for each denied request, got %d: %v", deniedAuditCount, err)
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='supervisor' WHERE admin_id=?", adminID)
	check("/api/v1/guardians/delete", http.StatusNoContent)

	// Missing duty is denied, including the former implicit supervisor case.
	mustExec(t, pool, "DELETE FROM care_staff_permission WHERE admin_id=?", adminID)
	check("/api/v1/device/list", http.StatusForbidden)
}

// Standalone endpoints must enforce their own read-time duty, even when the
// generic route gate is absent or a request authenticated before demotion.
func TestLegacySensitiveReadsRequireCurrentSupervisor(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 41, "supervisor")
	seedCareAdminDuty(t, pool, 42, "caregiver")
	h := testHandler(t, pool)
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name,customer_phone) VALUES(1,'保护的监护人','13800000000')")
	for _, tc := range []struct {
		actor   int64
		allowed bool
	}{
		{41, true},
		{42, false},
	} {
		for _, handler := range []http.HandlerFunc{h.listGuardians, h.securityLoginAudit} {
			wrapped := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: tc.actor}, handler)
			res := callAPI(wrapped, map[string]any{})
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusOK
			}
			if res.Code != want {
				t.Fatalf("actor=%d HTTP=%d want=%d: %s", tc.actor, res.Code, want, res.Body.String())
			}
		}
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=41")
	for _, handler := range []http.HandlerFunc{h.listGuardians, h.securityLoginAudit} {
		res := callAPI(withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 41}, handler), map[string]any{})
		if res.Code != http.StatusForbidden {
			t.Fatalf("formerly privileged session retained access: %d %s", res.Code, res.Body.String())
		}
	}
}

// Direct handlers recheck live duty inside each sensitive read or mutation.
func TestLegacyDeviceHealthAndGuardianAccessAfterDutyRevocation(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	seedCareAdminDuty(t, pool, 12, "caregiver")
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'initial')")
	h := testHandler(t, pool)
	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 11}
	readDevice := withHealthPrincipal(h, supervisor, h.deviceList)
	if got := callAPI(readDevice, map[string]any{}); got.Code != http.StatusOK {
		t.Fatalf("authorized device inventory: %d %s", got.Code, got.Body.String())
	}
	if got := callAPI(withHealthPrincipal(h, supervisor, h.deviceBindCodeList), map[string]any{"oldId": "1"}); got.Code != http.StatusOK {
		t.Fatalf("authorized bind-code inventory: %d %s", got.Code, got.Body.String())
	}
	healthQuery := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": "2026-10-20T00:00:00Z", "to": "2026-10-21T00:00:00Z"}
	if got := callAPI(withHealthPrincipal(h, supervisor, h.healthStatistics), healthQuery); got.Code != http.StatusOK {
		t.Fatalf("authorized health statistics: %d %s", got.Code, got.Body.String())
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=11")
	for _, actor := range []int64{11, 12} {
		p := session.Principal{Role: auth.RoleAdmin, ID: actor}
		for _, test := range []struct {
			name    string
			handler http.HandlerFunc
			body    map[string]any
		}{
			{"device_list", h.deviceList, map[string]any{}},
			{"resident_list", h.listResidents, map[string]any{"name": ""}},
			{"resident_details", h.getResidentDetails, map[string]any{"name": ""}},
			{"position_list", h.positionSelect, map[string]any{"name": ""}},
			{"position_summary", h.positionAlertSummary, map[string]any{}},
			{"position_resident", h.positionSelectFamily, map[string]any{"oldId": 1}},
			{"fence_save", h.fenceSave, map[string]any{
				"deviceId": 1, "revision": 0, "name": "restricted fence", "coordinateSystem": "WGS84",
				"timezone": "Asia/Shanghai", "timeBegin": 0, "timeEnd": 86400,
				"minPoints": 2, "dwellSeconds": 1, "maxAccuracy": 10,
				"coordinates": []any{[]float64{121, 31}, []float64{121.1, 31},
					[]float64{121.1, 31.1}, []float64{121, 31.1}, []float64{121, 31}},
			}},
			{"fence_list", h.fenceList, map[string]any{"deviceId": 1}},
			{"fence_events", h.fenceEvents, map[string]any{"oldId": 1}},
			{"command_profile", h.commandProfile, map[string]any{"deviceId": 1, "wireType": "K1", "protocol": "yihe-pull-v1", "enabled": true}},
			{"resident_create", h.createResident, elderPayload()},
			{"resident_update", h.updateResident, func() map[string]any { b := elderPayload(); b["oldId"] = 1; return b }()},
			{"resident_delete", h.deleteResident, map[string]any{"oldId": 1}},
			{"threshold_list", h.listMonitoringProfiles, map[string]any{"oldName": ""}},
			{"threshold_create", h.createMonitoringProfile, map[string]any{"deviceId": 1, "hrf": 100, "hrl": 60, "bpf": 140, "bpl": 90}},
			{"threshold_update", h.updateMonitoringProfile, map[string]any{"deviceId": 1, "hrf": 100, "hrl": 60, "bpf": 140, "bpl": 90}},
			{"threshold_delete", h.deleteMonitoringProfile, map[string]any{"deviceId": 1}},
			{"health_statistics", h.healthStatistics, healthQuery},
			{"health_changes", h.healthChanges, map[string]any{"oldId": 1}},
			{"health_latest", h.healthLatest, map[string]any{"oldId": 1}},
			{"health_baseline", h.healthBaseline, map[string]any{"oldId": 1}},
			{"vitals_latest", h.listLatestVitals, map[string]any{}},
			{"activity_latest", h.listLatestActivity, map[string]any{}},
			{"vitals_history", h.listVitalHistory, map[string]any{"oldId": 1}},
			{"vitals_chart", h.getVitalChart, map[string]any{"oldId": 1}},
			{"activity_history", h.listActivityHistory, map[string]any{"oldId": 1}},
			{"activity_chart", h.getActivityChart, map[string]any{"oldId": 1}},
			{"vitals_alert_detail", h.getVitalAlert, map[string]any{"oldId": 1}},
			{"vitals_alert_resolve", h.handleResolveVitalAlert, map[string]any{"recordId": 1}},
			{"vitals_alert_count", h.countVitalAlerts, map[string]any{}},
			{"health_alert_device_counts", h.countHealthAlertDevices, map[string]any{}},
			{"vitals_alert_device_counts", h.countVitalAlertDevices, map[string]any{}},
			{"sleep_alert_device_counts", h.countSleepAlertDevices, map[string]any{}},
			{"guardian_create", h.createGuardian, map[string]any{"customerName": "new", "customerAge": "40", "customerSex": "女", "customerPhone": "13800000000", "customerAddress": "test"}},
			{"guardian_update", h.updateGuardian, map[string]any{"customerId": 1, "customerName": "new", "customerAge": "40", "customerSex": "女", "customerPhone": "13800000000", "customerAddress": "test"}},
			{"guardian_delete", h.deleteGuardian, map[string]any{"customerId": 1}},
			{"bind_code_create", h.deviceCreateBindCode, map[string]any{"oldId": "1"}},
			{"bind_code_list", h.deviceBindCodeList, map[string]any{"oldId": "1"}},
			{"bind_code_delete", h.deviceDeleteBindCode, map[string]any{"id": "1"}},
			{"device_secret_reset", h.deviceResetSecret, map[string]any{"deviceId": "1"}},
			{"device_unbind", h.deviceUnbind, map[string]any{"deviceId": "1"}},
			{"device_delete", h.deviceDelete, map[string]any{"deviceId": "1"}},
		} {
			got := callAPI(withHealthPrincipal(h, p, test.handler), test.body)
			if got.Code != http.StatusForbidden {
				t.Fatalf("actor %d %s: HTTP %d %s", actor, test.name, got.Code, got.Body.String())
			}
		}
	}
	var thresholdCount int
	if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM watch_info").Scan(&thresholdCount); err != nil || thresholdCount != 0 {
		t.Fatalf("unauthorized mutation changed monitoring profiles: count=%d err=%v", thresholdCount, err)
	}
	var customerCount int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM customer").Scan(&customerCount); err != nil || customerCount != 1 {
		t.Fatalf("unauthorized mutation changed guardian records: count=%d err=%v", customerCount, err)
	}
	var deviceCount int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM device WHERE device_id=1 AND old_id=1").Scan(&deviceCount); err != nil || deviceCount != 1 {
		t.Fatalf("unauthorized mutation changed device binding: count=%d err=%v", deviceCount, err)
	}
}

func TestRestrictedAdminRouteAllowlist(t *testing.T) {
	for _, path := range []string{
		"/api/v1/care/receipt",
		"/api/v1/care/newPrivilegedAction",
		"/api/v1/guardians/delete",
		"/api/v1/notifications/search",
	} {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		if restrictedAdminRouteAllowed(req) {
			t.Fatalf("unexpected allowlist entry: %s", path)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/care/incidents", nil)
	if restrictedAdminRouteAllowed(req) {
		t.Fatal("wrong HTTP method bypassed duty gate")
	}
}

func TestRestrictedAdminGateFailsClosedWithoutAuditStore(t *testing.T) {
	pool := testDatabase(t)
	const id int64 = 987655
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role,user_phone) VALUES(?,'AuditFail','1','13900009875')", id)
	mustExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(?,'auditor',?)", id, id)
	// This is an isolated per-test database, never production.
	mustExec(t, pool, "DROP TABLE care_sensitive_access_audit")
	sessions := session.NewStore(time.Hour)
	sess := sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: id})
	nextCalled := false
	gate := auth.Middleware(sessions, testHandler(t, pool).EnforceAdminDuty(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			nextCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
	)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/guardians/delete", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || nextCalled {
		t.Fatalf("missing audit store must fail closed: status=%d nextCalled=%v body=%s",
			rec.Code, nextCalled, rec.Body.String())
	}
}
