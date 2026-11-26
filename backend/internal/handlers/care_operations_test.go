package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/session"
)

// MySQL authorization regression: a family session is denied by
// careAdmin; these checks cover assigned administrative duties.
func TestCareDutyPermissionsAndSensitiveAudit(t *testing.T) {
	pool := testDatabase(t)
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(1,'主管','1'),(2,'护理员','1'),(3,'审计员','1')")
	mustExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'supervisor',1),(2,'caregiver',1),(3,'auditor',1)")
	h := testHandler(t, pool)
	req := httptest.NewRequest("POST", "/api/v1/care/transition", nil)
	for _, tc := range []struct {
		actor  int64
		action string
		want   bool
	}{
		{1, "verify", true},
		{2, "acknowledge", true},
		{2, "resolve", true},
		{2, "verify", false},
		{3, "acknowledge", false},
		{3, "verify", false},
	} {
		w := httptest.NewRecorder()
		got := h.authorizeCareAction(w, req, tc.actor, tc.action)
		if got != tc.want {
			t.Errorf("duty actor=%d action=%s got=%v want=%v", tc.actor, tc.action, got, tc.want)
		}
	}
	w := httptest.NewRecorder()
	if !h.auditCareAccess(w, req, 2, "incident_list", nil) {
		t.Fatalf("audit failed: %s", w.Body.String())
	}
	var count int64
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM care_access_audit WHERE admin_id=2 AND action='incident_list'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit not durable: count=%d err=%v", count, err)
	}
}

func TestRoleSpecificCarePermissions(t *testing.T) {
	pool := testDatabase(t)
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(11,'主管','1'),(12,'护理员','1'),(13,'审计员','1'),(14,'未授权','1')")
	mustExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(11,'supervisor',11),(12,'caregiver',11),(13,'auditor',11)")
	h := testHandler(t, pool)
	req := httptest.NewRequest("POST", "/api/v1/care/incidents", nil)
	for _, tc := range []struct {
		actor   int64
		action  string
		allowed bool
	}{
		{11, "read_tasks", true},
		{11, "manage_staff", true},
		{11, "verify", true},
		{12, "read_tasks", true},
		{12, "acknowledge", true},
		{12, "resolve", true},
		{12, "read_audit", false},
		{12, "verify", false},
		{13, "read_tasks", true},
		{13, "read_audit", true},
		{13, "diagnostics", false},
		{13, "resolve", false},
		{14, "read_tasks", false},
		{14, "read_audit", false},
		{14, "verify", false},
	} {
		recorder := httptest.NewRecorder()
		got := h.authorizeCareAction(recorder, req, tc.actor, tc.action)
		if got != tc.allowed {
			t.Errorf("actor %d action %s got %v want %v", tc.actor, tc.action, got, tc.allowed)
		}
		if !got && recorder.Code != 403 {
			t.Errorf("actor %d action %s denied with %d, want 403", tc.actor, tc.action, recorder.Code)
		}
	}
}

func TestDemotedAdminCannotReuseStoredSupervisorDuty(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 21, "supervisor")
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(22,'护理员','1')")
	mustExec(t, pool, "UPDATE users SET role='0' WHERE user_id=21")
	h := testHandler(t, pool)
	req := httptest.NewRequest("POST", "/api/v1/care/staff", nil)
	w := httptest.NewRecorder()
	if h.authorizeCareAction(w, req, 21, "manage_staff") || w.Code != 403 {
		t.Fatalf("demoted account retained supervisor access: HTTP %d", w.Code)
	}
	if err := careworkflow.AssignDuty(context.Background(), pool, 21, 22, "caregiver"); !errors.Is(err, careworkflow.ErrDutyDenied) {
		t.Fatalf("demoted account assigned staff duty: %v", err)
	}
}

func TestCareStaffListExcludesNonAdministrators(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(12,'普通用户','0')")
	h := testHandler(t, pool)
	result := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.careStaff), map[string]any{})
	staff := result["staff"].([]any)
	if len(staff) != 1 || staff[0].(map[string]any)["id"] != float64(11) {
		t.Fatalf("non-admin included in care staff: %v", staff)
	}
}

// A read already holding the role lock finishes before reassignment commits.
// New requests after reassignment cannot reuse the previous audit privileges.
func TestSensitiveReadSerializesConcurrentDutyChange(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 31, "auditor")
	seedCareAdminDuty(t, pool, 32, "supervisor")
	h := testHandler(t, pool)
	req := httptest.NewRequest("POST", "/api/v1/care/accessAudit", nil)
	w := httptest.NewRecorder()
	tx, duty, ok := h.beginCareRead(w, req, 31, carepolicy.ReadAudit)
	if !ok || duty != carepolicy.Auditor {
		t.Fatalf("authorized reader rejected: %d %s", w.Code, w.Body.String())
	}
	defer tx.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- careworkflow.AssignDuty(ctx, pool, 32, 31, "caregiver") }()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("role change completed before sensitive read ended: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := careworkflow.AssignDuty(context.Background(), pool, 32, 31, "caregiver"); err != nil {
		t.Fatal(err)
	}
	denied := careRequest(t, h, h.careAccessAudit, 31, map[string]any{})
	if denied.Code != 403 {
		t.Fatalf("former auditor accessed audit after demotion: %d %s", denied.Code, denied.Body.String())
	}
}

func TestIncidentHistoryLockFencesConcurrentOwnershipChange(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 41, "caregiver")
	seedCareAdminDuty(t, pool, 42, "caregiver")
	mustExec(t, pool, "INSERT INTO care_incident(id,old_id,device_id,kind,state,severity,acknowledged_by) VALUES(9,1,1,'sos','in_progress','critical','41')")
	tx, err := pool.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	visible, err := careworkflow.CanReadIncident(context.Background(), tx, carepolicy.Caregiver, 41, 9)
	if err != nil || !visible {
		t.Fatalf("current caregiver denied task: visible=%v err=%v", visible, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := pool.ExecContext(ctx, "UPDATE care_incident SET acknowledged_by='42' WHERE id=9")
		done <- err
	}()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("incident owner changed during locked history read: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, "UPDATE care_incident SET acknowledged_by='42' WHERE id=9")
	h := testHandler(t, pool)
	if got := careRequest(t, h, h.careEvents, 41, map[string]any{"id": "9"}); got.Code != 403 {
		t.Fatalf("previous owner read incident history: HTTP %d %s", got.Code, got.Body.String())
	}
	if got := careRequest(t, h, h.careEvents, 42, map[string]any{"id": "9"}); got.Code != 200 {
		t.Fatalf("new owner denied incident history: HTTP %d %s", got.Code, got.Body.String())
	}
}

func TestCareWorkerHeartbeatSchema(t *testing.T) {
	pool := testDatabase(t)
	mustExec(t, pool, "INSERT INTO care_worker_heartbeat(singleton,last_seen_at,started_at) VALUES(true,CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))")
	var healthy bool
	if err := pool.QueryRowContext(context.Background(),
		"SELECT last_seen_at > CURRENT_TIMESTAMP(6)-INTERVAL 30 SECOND FROM care_worker_heartbeat WHERE singleton=true").Scan(&healthy); err != nil || !healthy {
		t.Fatalf("worker heartbeat unavailable: %v", err)
	}
}

func TestCareDutyAssignmentTransaction(t *testing.T) {
	pool := testDatabase(t)
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(101,'主管','1'),(102,'护理员','1'),(103,'审计员','1')")
	mustExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(101,'supervisor',101)")
	ctx := context.Background()

	if err := careworkflow.AssignDuty(ctx, pool, 101, 102, "caregiver"); err != nil {
		t.Fatal(err)
	}
	var duty string
	var audits int
	err := pool.QueryRowContext(ctx, "SELECT duty FROM care_staff_permission WHERE admin_id=?", 102).Scan(&duty)
	if err != nil || duty != "caregiver" {
		t.Fatalf("duty not committed: %q %v", duty, err)
	}
	err = pool.QueryRowContext(ctx, "SELECT count(*) FROM care_access_audit WHERE admin_id=101 AND action='staff_duty_changed' AND subject_admin_id=102").Scan(&audits)
	if err != nil || audits != 1 {
		t.Fatalf("permission audit missing: %d %v", audits, err)
	}
	if err := careworkflow.AssignDuty(ctx, pool, 102, 103, "auditor"); !errors.Is(err, careworkflow.ErrDutyDenied) {
		t.Fatalf("caregiver assigned another duty: %v", err)
	}
	if err := careworkflow.AssignDuty(ctx, pool, 101, 999999, "auditor"); !errors.Is(err, careworkflow.ErrStaffMissing) {
		t.Fatalf("missing administrator was accepted: %v", err)
	}
	err = pool.QueryRowContext(ctx, "SELECT count(*) FROM care_access_audit WHERE admin_id=101 AND action='staff_duty_changed'").Scan(&audits)
	if err != nil || audits != 1 {
		t.Fatalf("failed assignment wrote an audit: %d %v", audits, err)
	}
}

func TestFirstAdministratorReceivesExplicitSupervisor(t *testing.T) {
	pool := testDatabase(t)
	ctx := context.Background()
	if err := authrepo.InitializeAdmin(ctx, pool, nil, "主管", "13900001111", "SecureCare2026"); err != nil {
		t.Fatal(err)
	}
	var adminID int64
	var duty string
	if err := pool.QueryRowContext(ctx, "SELECT u.user_id,p.duty FROM users u JOIN care_staff_permission p ON p.admin_id=u.user_id WHERE u.user_phone=?", "13900001111").Scan(&adminID, &duty); err != nil || duty != "supervisor" {
		t.Fatalf("first administrator lacks explicit supervisor: id=%d duty=%q err=%v", adminID, duty, err)
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_access_audit WHERE admin_id=? AND action='supervisor_initialized' AND subject_admin_id=?", adminID, adminID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("initial supervisor audit missing: %d %v", count, err)
	}
	if err := authrepo.InitializeAdmin(ctx, pool, nil, "后续", "13900002222", "SecureCare2026"); !errors.Is(err, authrepo.ErrAlreadyInitialized) {
		t.Fatalf("first-run setup remained open: %v", err)
	}
}
