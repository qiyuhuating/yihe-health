package handlers

import (
	"net/http"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestResidentLocationCommittedAudit(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001'),(2,'13800000002')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)
	const resource = "resident-location:committed:1"
	call := func(p session.Principal) *http.Response {
		return callAPI(withHealthPrincipal(h, p, h.positionSelectFamily), map[string]any{"oldId": 1}).Result()
	}
	family := session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}
	other := session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 1}
	admin := session.Principal{Role: auth.RoleAdmin, ID: 71}
	count := func(p session.Principal, want int) {
		t.Helper()
		var n int
		if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM care_sensitive_access_audit WHERE actor_id=? AND actor_role=? AND resource=? AND method='POST'", p.ID, p.Role, resource).Scan(&n); err != nil || n != want {
			t.Fatalf("audit count for %s/%d = %d, want %d, err=%v", p.Role, p.ID, n, want, err)
		}
	}
	if got := call(family); got.StatusCode != http.StatusOK {
		t.Fatal("authorized family query failed", got.StatusCode)
	}
	if got := call(admin); got.StatusCode != http.StatusOK {
		t.Fatal("authorized administrator query failed", got.StatusCode)
	}
	if got := call(other); got.StatusCode != http.StatusForbidden {
		t.Fatal("cross-resident query was not denied", got.StatusCode)
	}
	count(family, 1)
	count(admin, 1)
	count(other, 0)
	mustExec(t, pool, `CREATE TRIGGER reject_location_audit BEFORE INSERT ON care_sensitive_access_audit
	FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test audit failure'`)
	w := callAPI(withHealthPrincipal(h, family, h.positionSelectFamily), map[string]any{"oldId": 1})
	if w.Code < 500 || strings.Contains(w.Body.String(), "positionDtos") {
		t.Fatalf("failed audit allowed response: %d %s", w.Code, w.Body.String())
	}
	count(family, 1)
}
