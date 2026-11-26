package handlers

import (
	"context"
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/rules"
	"yihe-care/internal/session"
)

func TestRulePolicyAndOperationsRequireLiveSupervisor(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	h := testHandler(t, pool)

	check := func(actor int64, handler http.HandlerFunc, body map[string]any, want int) {
		t.Helper()
		p := session.Principal{Role: auth.RoleAdmin, ID: actor}
		got := callAPI(withHealthPrincipal(h, p, handler), body)
		if got.Code != want {
			t.Fatalf("actor %d: HTTP %d, want %d: %s", actor, got.Code, want, got.Body.String())
		}
	}
	read := map[string]any{"deviceId": 1}
	write := map[string]any{"deviceId": 1, "revision": 0, "policy": rules.Default()}
	for _, actor := range []int64{71, 72} {
		want := http.StatusForbidden
		if actor == 71 {
			want = http.StatusOK
		}
		check(actor, h.rulePolicy, read, want)
		check(actor, h.operationsSnapshot, map[string]any{}, want)
	}

	// A valid policy write must commit before the supervisor is demoted.
	check(71, h.rulePolicy, write, http.StatusOK)
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")

	// Direct handlers do not rely on the legacy middleware's earlier check.
	for _, actor := range []int64{71, 72} {
		check(actor, h.rulePolicy, read, http.StatusForbidden)
		check(actor, h.rulePolicy, write, http.StatusForbidden)
		check(actor, h.operationsSnapshot, map[string]any{}, http.StatusForbidden)
	}
	var revision int64
	if err := pool.QueryRowContext(context.Background(),
		"SELECT revision FROM rule_policy WHERE device_id=1").Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("unauthorized policy write changed revision: revision=%d err=%v", revision, err)
	}
}

func TestLegacyDeviceCommandsAndReviewsRequireLiveSupervisor(t *testing.T) {
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
		{"command_list", h.commandList, map[string]any{"deviceId": 1}},
		{"command_create", h.commandCreate, map[string]any{"deviceId": 1, "action": "locate", "requestKey": "denied-test", "ttlSeconds": 60}},
		{"command_profile", h.commandProfile, map[string]any{"deviceId": 1, "wireType": "K1", "protocol": "yihe-pull-v1", "enabled": true}},
		{"alert_reviews", h.alertReviews, map[string]any{"entity": "ratepre", "recordId": 1, "kind": "rate"}},
		{"alert_review", h.alertReview, map[string]any{"recordId": 1, "kind": "rate", "verdict": "confirmed", "reason": "verified", "expectedReviewId": 0}},
	}
	check := func(actor int64, tc struct {
		name    string
		handler http.HandlerFunc
		body    map[string]any
	}, want int) {
		t.Helper()
		p := session.Principal{Role: auth.RoleAdmin, ID: actor}
		got := callAPI(withHealthPrincipal(h, p, tc.handler), tc.body)
		if got.Code != want {
			t.Fatalf("%s actor=%d: HTTP %d, want %d: %s", tc.name, actor, got.Code, want, got.Body.String())
		}
	}

	// Legitimate list/read behavior remains available to a current supervisor.
	check(71, cases[0], http.StatusOK)
	check(71, cases[2], http.StatusOK)

	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	for _, tc := range cases {
		for _, actor := range []int64{71, 72} {
			check(actor, tc, http.StatusForbidden)
		}
	}
	for _, table := range []string{"device_command", "alert_review"} {
		var count int64
		if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s gained unauthorized records: count=%d err=%v", table, count, err)
		}
	}
}
