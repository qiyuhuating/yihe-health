package handlers

import (
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestFamilyProfileWritesRequireLiveDutyOrOwnAccount(t *testing.T) {
	store := testDatabase(t)
	seedCareAdminDuty(t, store, 71, "supervisor")
	seedCareAdminDuty(t, store, 72, "caregiver")
	mustExec(t, store, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'家属一','13800000001'),(2,'家属二','13800000002')")
	h := testHandler(t, store)
	body := func(id int64, phone string) map[string]any {
		return map[string]any{"familyId": id, "familyPhone": phone, "familyName": "新名称", "familySex": "女", "familyAddress": "新地址"}
	}
	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	caregiver := session.Principal{Role: auth.RoleAdmin, ID: 72}
	family := session.Principal{Role: auth.RoleFamily, ID: 1, Phone: "13800000001"}

	for _, tc := range []struct {
		p      session.Principal
		body   map[string]any
		status int
	}{
		{supervisor, body(2, "13800000002"), http.StatusOK},
		{caregiver, body(2, "13800000002"), http.StatusForbidden},
		{family, body(1, "13800000001"), http.StatusOK},
		{family, body(2, "13800000001"), http.StatusForbidden},
	} {
		got := callAPI(withHealthPrincipal(h, tc.p, h.familyUpdateTwo), tc.body)
		if got.Code != tc.status {
			t.Fatalf("actor=%s/%d HTTP %d want %d: %s", tc.p.Role, tc.p.ID, got.Code, tc.status, got.Body.String())
		}
	}
	mustExec(t, store, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	if got := callAPI(withHealthPrincipal(h, supervisor, h.familyUpdateTwo), body(2, "13800000002")); got.Code != http.StatusForbidden {
		t.Fatalf("revoked supervisor edited family profile: HTTP %d %s", got.Code, got.Body.String())
	}
	mustExec(t, store, "DELETE FROM family WHERE family_id=1")
	if got := callAPI(withHealthPrincipal(h, family, h.familyUpdateTwo), body(1, "13800000001")); got.Code != http.StatusForbidden {
		t.Fatalf("deleted family reused stale session: HTTP %d %s", got.Code, got.Body.String())
	}
}
