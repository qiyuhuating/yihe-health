package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/risk"
	"yihe-care/internal/session"
)

func TestRiskHandlersRequireLiveDutyAndAtomicAudit(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	h.Risk = risk.New()
	t.Setenv("YIHE_RISK_AUDIT_SPOOL_PATH", filepath.Join(t.TempDir(), "audit.jsonl"))
	if err := h.Risk.EnableMySQL(pool.DB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Risk.CloseMySQL() })
	request := func(handler http.HandlerFunc, actor int64, body map[string]any) *httptest.ResponseRecorder {
		sess := h.Sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: actor})
		return callAPI(func(w http.ResponseWriter, r *http.Request) {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
			handler(w, r)
		}, body)
	}
	for id, duty := range map[int64]string{1: "supervisor", 2: "caregiver", 3: "auditor", 5: "supervisor", 6: "supervisor"} {
		seedCareAdminDuty(t, pool, id, duty)
	}
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(4,'unassigned','1')")
	mustExec(t, pool, "UPDATE users SET account_status='suspended' WHERE user_id=5")
	mustExec(t, pool, "UPDATE users SET role='0' WHERE user_id=6")
	input := map[string]any{"ip": "203.0.113.9", "action": "skip-auto", "reason": strings.Repeat("r", 240)}
	for _, actor := range []int64{2, 3, 4, 5, 6} {
		got := request(h.riskAdminRuleCreate, actor, input)
		if got.Code != http.StatusForbidden {
			t.Fatalf("actor %d: HTTP %d %s", actor, got.Code, got.Body.String())
		}
	}
	got := request(h.riskAdminRuleCreate, 1, input)
	if got.Code != http.StatusOK {
		t.Fatalf("valid rule: HTTP %d %s", got.Code, got.Body.String())
	}
	var id, actor string
	if err := pool.QueryRowContext(context.Background(), "SELECT rule_id FROM risk_ip_rules").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT account_mask FROM risk_login_events WHERE event_type='rule_created'").Scan(&actor); err != nil || actor != "admin:1" {
		t.Fatalf("missing audit actor: %q %v", actor, err)
	}
	mustExec(t, pool, "CREATE TRIGGER reject_risk_audit BEFORE INSERT ON risk_login_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected failure'")
	got = request(h.riskAdminRuleCreate, 1, input)
	if got.Code != http.StatusServiceUnavailable {
		t.Fatalf("audit storage failure: HTTP %d %s", got.Code, got.Body.String())
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM risk_ip_rules").Scan(&count); err != nil || count != 1 {
		t.Fatalf("handler failed to roll back: %d %v", count, err)
	}
	mustExec(t, pool, "DROP TRIGGER reject_risk_audit")
	remove := func(w http.ResponseWriter, r *http.Request) { r.SetPathValue("id", id); h.riskAdminRuleDelete(w, r) }
	if got := request(remove, 2, nil); got.Code != http.StatusForbidden {
		t.Fatal("caregiver deleted rule", got.Code)
	}
	if got := request(remove, 1, nil); got.Code != http.StatusOK {
		t.Fatalf("delete: HTTP %d %s", got.Code, got.Body.String())
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM risk_login_events").Scan(&count); err != nil || count != 2 {
		t.Fatalf("orphan/missing audit: %d %v", count, err)
	}
}
