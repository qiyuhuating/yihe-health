package handlers

import (
	"context"
	"net/http"
	"testing"
)

func TestCareManualEscalationQueuesFallbackAtomically(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	t.Setenv("CARE_ESCALATION_PHONE", "13800000002")
	h := testHandler(t, pool)
	ctx := context.Background()
	mustExec(t, pool, "INSERT INTO care_incident(id,old_id,device_id,kind,state,severity,due_at) VALUES(1,1,1,'sos','received','critical',CURRENT_TIMESTAMP(6))")
	call := func() int {
		return careRequest(t, h, h.careTransition, 1, map[string]any{"id": "1", "action": "escalate"}).Code
	}
	check := func(wantState string, wantEvents, wantNotices int) {
		t.Helper()
		var state string
		var events, notices int
		if err := pool.QueryRowContext(ctx, "SELECT state FROM care_incident WHERE id=1").Scan(&state); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_incident_event WHERE incident_id=1 AND action='escalate'").Scan(&events); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_notification_outbox WHERE incident_id=1 AND status='pending'").Scan(&notices); err != nil {
			t.Fatal(err)
		}
		if state != wantState || events != wantEvents || notices != wantNotices {
			t.Fatalf("state=%s events=%d notices=%d; want %s %d %d", state, events, notices, wantState, wantEvents, wantNotices)
		}
	}
	mustExec(t, pool, "CREATE TRIGGER reject_escalation_notice BEFORE INSERT ON care_notification_outbox FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test failure'")
	if got := call(); got != http.StatusInternalServerError {
		t.Fatalf("failed enqueue returned %d", got)
	}
	check("received", 0, 0)
	mustExec(t, pool, "DROP TRIGGER reject_escalation_notice")
	if got := call(); got != http.StatusOK {
		t.Fatalf("escalation returned %d", got)
	}
	if got := call(); got != http.StatusConflict {
		t.Fatalf("duplicate escalation returned %d", got)
	}
	check("escalated", 1, 1)
}

func TestCareIndependentReviewRejectsTaskOwner(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	for _, actor := range []int64{1, 2, 3} {
		seedCareAdminDuty(t, pool, actor, "supervisor")
	}
	h := testHandler(t, pool)
	mustExec(t, pool, "INSERT INTO care_incident(id,old_id,device_id,kind,state,severity,due_at,acknowledged_by,resolved_by) VALUES(1,1,1,'sos','resolved','critical',CURRENT_TIMESTAMP(6),'1','2')")
	for _, actor := range []int64{1, 2, 3} {
		out := careRequest(t, h, h.careTransition, actor, map[string]any{"id": "1", "action": "verify", "note": "verified"})
		want := http.StatusForbidden
		if actor == 3 {
			want = http.StatusOK
		}
		if out.Code != want {
			t.Fatalf("actor=%d got %d want %d: %s", actor, out.Code, want, out.Body.String())
		}
	}
	var reviewer string
	if err := pool.QueryRowContext(context.Background(), "SELECT verified_by FROM care_incident WHERE id=1 AND state='verified'").Scan(&reviewer); err != nil || reviewer != "3" {
		t.Fatalf("independent review not recorded: %q %v", reviewer, err)
	}
}
