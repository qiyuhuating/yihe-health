package handlers

import (
	"context"
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestNotificationsRequireLiveSupervisorOrCurrentFamily(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001'),(2,'13800000002')")
	mustExec(t, pool, `INSERT INTO message_outbox(topic,source_key,family_id,channel,recipient)
 VALUES('alarm','family-one',1,'in_app','one'),('alarm','family-two',2,'in_app','two')`)
	h := testHandler(t, pool)
	read := func(p session.Principal, requested int64, want int, source string) {
		t.Helper()
		got := callAPI(withHealthPrincipal(h, p, h.notifications), map[string]any{"familyId": requested})
		if got.Code != want {
			t.Fatalf("actor %s/%d HTTP %d, want %d: %s", p.Role, p.ID, got.Code, want, got.Body.String())
		}
		if want != http.StatusOK {
			return
		}
		decoded := api(t, withHealthPrincipal(h, p, h.notifications), map[string]any{"familyId": requested})
		items := decoded["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["sourceId"] != source {
			t.Fatalf("actor %s/%d saw wrong message scope: %v", p.Role, p.ID, items)
		}
	}
	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	caregiver := session.Principal{Role: auth.RoleAdmin, ID: 72}
	family := session.Principal{Role: auth.RoleFamily, ID: 1}
	read(supervisor, 2, http.StatusOK, "family-two")
	read(caregiver, 2, http.StatusForbidden, "")
	read(family, 2, http.StatusOK, "family-one")

	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	read(supervisor, 2, http.StatusForbidden, "")
	mustExec(t, pool, "DELETE FROM family WHERE family_id=1")
	read(family, 1, http.StatusForbidden, "")

	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM message_outbox").Scan(&count); err != nil || count != 2 {
		t.Fatalf("read authorization changed messages: count=%d err=%v", count, err)
	}
}
