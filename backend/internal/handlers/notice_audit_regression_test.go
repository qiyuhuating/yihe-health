package handlers

import (
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestNoticeEditAndDeleteHaveDistinctAtomicAudits(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	mustExec(t, pool, "INSERT INTO `text`(text_id,title,text_main,time) VALUES(1,'Initial','First',CURRENT_TIMESTAMP(6))")
	h := testHandler(t, pool)
	actor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	edit := withHealthPrincipal(h, actor, h.updateNotice)
	remove := withHealthPrincipal(h, actor, h.deleteNotice)
	payload := map[string]any{"textId": 1, "title": "Updated", "textMain": "Second", "fileListName": []string{}}

	if got := callAPI(edit, payload); got.Code != http.StatusOK {
		t.Fatalf("text-only update failed: HTTP %d %s", got.Code, got.Body.String())
	}
	count := func(action string) int {
		t.Helper()
		var n int
		if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM care_access_audit WHERE admin_id=71 AND action=?", action).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("notice_updated") != 1 || count("notice_deleted") != 0 {
		t.Fatalf("edit audit polluted deletion history: updated=%d deleted=%d", count("notice_updated"), count("notice_deleted"))
	}

	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	for _, tc := range []struct {
		handler http.HandlerFunc
		body    map[string]any
	}{
		{edit, payload},
		{remove, map[string]any{"textId": 1}},
	} {
		got := callAPI(tc.handler, tc.body)
		if got.Code != http.StatusForbidden {
			t.Fatalf("revoked supervisor mutated notice: HTTP %d %s", got.Code, got.Body.String())
		}
	}
	if count("notice_updated") != 1 || count("notice_deleted") != 0 {
		t.Fatal("denied operations wrote notice audit")
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='supervisor' WHERE admin_id=71")
	if got := callAPI(remove, map[string]any{"textId": 1}); got.Code != http.StatusOK {
		t.Fatalf("notice delete failed: HTTP %d %s", got.Code, got.Body.String())
	}
	if count("notice_updated") != 1 || count("notice_deleted") != 1 {
		t.Fatalf("delete audit missing: updated=%d deleted=%d", count("notice_updated"), count("notice_deleted"))
	}
}
