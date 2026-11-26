package handlers

import (
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestFamilyContactsHonorLiveDutyAndAccountOwnership(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'家属甲','13800000001')")
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,family_id) VALUES(1,'老人甲',1)")
	h := testHandler(t, pool)
	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	caregiver := session.Principal{Role: auth.RoleAdmin, ID: 72}
	family := session.Principal{Role: auth.RoleFamily, ID: 1, Phone: "13800000001"}

	for _, tc := range []struct {
		p      session.Principal
		status int
	}{
		{supervisor, http.StatusOK},
		{caregiver, http.StatusForbidden},
	} {
		got := callAPI(withHealthPrincipal(h, tc.p, h.familySelect), map[string]any{"familyName": ""})
		if got.Code != tc.status {
			t.Fatalf("%s list HTTP %d, want %d: %s", tc.p.Role, got.Code, tc.status, got.Body.String())
		}
	}
	for _, tc := range []struct {
		p      session.Principal
		status int
	}{
		{supervisor, http.StatusOK},
		{caregiver, http.StatusForbidden},
		{family, http.StatusOK},
	} {
		got := callAPI(withHealthPrincipal(h, tc.p, h.familySelectTwo), map[string]any{"familyPhone": "13800000001"})
		if got.Code != tc.status {
			t.Fatalf("%s detail HTTP %d, want %d: %s", tc.p.Role, got.Code, tc.status, got.Body.String())
		}
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	for _, handler := range []http.HandlerFunc{h.familySelect, h.familySelectTwo} {
		got := callAPI(withHealthPrincipal(h, supervisor, handler), map[string]any{"familyPhone": "13800000001"})
		if got.Code != http.StatusForbidden {
			t.Fatalf("revoked supervisor read contacts: HTTP %d %s", got.Code, got.Body.String())
		}
	}
	mustExec(t, pool, "UPDATE oldperson SET family_id=NULL WHERE old_id=1")
	mustExec(t, pool, "DELETE FROM family WHERE family_id=1")
	got := callAPI(withHealthPrincipal(h, family, h.familySelectTwo), map[string]any{"familyPhone": "13800000001"})
	if got.Code != http.StatusForbidden {
		t.Fatalf("deleted family retained account record: HTTP %d %s", got.Code, got.Body.String())
	}
}
