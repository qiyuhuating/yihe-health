package careworkflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securityaudit"
)

var recoveryOperator = securityaudit.Actor{User: "operator", UID: "test-os-id", Host: "recovery-host"}

func TestSupervisorRecoveryAuditsDenialsAndGrant(t *testing.T) {
	store := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := store.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role,account_status) VALUES(1,'recover','1','active'),(2,'inactive','1','suspended')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(2,'supervisor',2)"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id               int64
		reason, decision string
	}{
		{1, "", "reason_required"}, {2, "INC-2026-recovery", "target_not_active_admin"},
	} {
		result, err := RecoverSupervisor(ctx, store, tc.id, recoveryOperator, tc.reason)
		if !errors.Is(err, ErrRecoveryDenied) || result.Decision != tc.decision || result.AuditID == "" {
			t.Fatalf("denial not audited: %+v %v", result, err)
		}
	}
	result, err := RecoverSupervisor(ctx, store, 1, recoveryOperator, "INC-2026-recovery")
	if err != nil {
		t.Fatal(err)
	}
	var actor securityaudit.Actor
	var reason, outcome string
	var at time.Time
	var beforeJSON, afterJSON []byte
	err = store.QueryRowContext(ctx, `SELECT os_user,os_uid,host,reason,outcome,occurred_at,before_state,after_state
 FROM security_audit_log WHERE id=?`, result.AuditID).Scan(&actor.User, &actor.UID, &actor.Host, &reason, &outcome, &at, &beforeJSON, &afterJSON)
	if err != nil || actor != recoveryOperator || reason != "INC-2026-recovery" || outcome != "granted" || at.IsZero() {
		t.Fatalf("recovery evidence incomplete: %+v %s %s %v", actor, reason, outcome, err)
	}
	var before, after securityaudit.Snapshot
	if err := json.Unmarshal(beforeJSON, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(afterJSON, &after); err != nil {
		t.Fatal(err)
	}
	if !before.Exists || before.Role != "1" || before.Status != "active" || before.Duty != "unassigned" || after.Duty != "supervisor" {
		t.Fatalf("incorrect before/after evidence: %+v %+v", before, after)
	}
	if again, err := RecoverSupervisor(ctx, store, 1, recoveryOperator, "INC-repeat"); !errors.Is(err, ErrRecoveryDenied) || again.Decision != "supervisor_exists" {
		t.Fatalf("existing supervisor recovery was not denied: %+v %v", again, err)
	}
	for _, table := range []string{"security_audit_log", "security_alert_outbox"} {
		var count int
		if err := store.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 4 {
			t.Fatalf("attempt missing from %s: %d %v", table, count, err)
		}
	}
	for _, query := range []string{"UPDATE security_audit_log SET reason='changed' WHERE id=?", "DELETE FROM security_audit_log WHERE id=?"} {
		_, err := store.ExecContext(ctx, query, result.AuditID)
		var sqlErr *mysql.MySQLError
		if !errors.As(err, &sqlErr) || sqlErr.Number != 1644 {
			t.Fatalf("audit mutation not rejected by append-only trigger: %v", err)
		}
	}
}

func TestSupervisorRecoveryRollsBackEvidenceFailures(t *testing.T) {
	for _, table := range []string{"security_audit_log", "security_alert_outbox"} {
		t.Run(table, func(t *testing.T) {
			store := dbtest.MySQL(t)
			ctx := context.Background()
			if _, err := store.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role) VALUES(1,'recover','1')"); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ExecContext(ctx, "CREATE TRIGGER fail_recovery_evidence BEFORE INSERT ON "+table+" FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='evidence unavailable'"); err != nil {
				t.Fatal(err)
			}
			if _, err := RecoverSupervisor(ctx, store, 1, recoveryOperator, "INC-rollback"); err == nil || errors.Is(err, ErrRecoveryDenied) {
				t.Fatalf("evidence failure did not fail recovery: %v", err)
			}
			for _, target := range []string{"care_staff_permission", "care_access_audit", "security_audit_log", "security_alert_outbox"} {
				var count int
				if err := store.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+target).Scan(&count); err != nil || count != 0 {
					t.Fatalf("failed recovery committed %s: %d %v", target, count, err)
				}
			}
		})
	}
}

func TestConcurrentSupervisorRecoveryGrantsOnlyOne(t *testing.T) {
	store := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := store.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role) VALUES(1,'first','1'),(2,'second','1')"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, id := range []int64{1, 2} {
		go func() {
			_, err := RecoverSupervisor(ctx, store, id, recoveryOperator, "INC-concurrent")
			results <- err
		}()
	}
	granted, denied := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			granted++
		case errors.Is(err, ErrRecoveryDenied):
			denied++
		default:
			t.Fatal(err)
		}
	}
	if granted != 1 || denied != 1 {
		t.Fatalf("granted=%d denied=%d", granted, denied)
	}
	var duties, audits, alerts int
	if err := store.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM care_staff_permission WHERE duty='supervisor'),
 (SELECT COUNT(*) FROM security_audit_log),(SELECT COUNT(*) FROM security_alert_outbox)`).Scan(&duties, &audits, &alerts); err != nil || duties != 1 || audits != 2 || alerts != 2 {
		t.Fatalf("recovery concurrency evidence: %d %d %d %v", duties, audits, alerts, err)
	}
}

func TestSupervisorRecoveryAuditSurvivesMigrationRollback(t *testing.T) {
	store := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := store.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role) VALUES(1,'recover','1')"); err != nil {
		t.Fatal(err)
	}
	result, err := RecoverSupervisor(ctx, store, 1, recoveryOperator, "INC-rollback-retention")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../migrations/rollback/mysql_0029_supervisor_recovery_audit.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(script), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := store.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.MigrateMySQL(ctx, store); err != nil {
		t.Fatal(err)
	}
	var auditID, alertStatus string
	if err := store.QueryRowContext(ctx, `SELECT a.id,o.status FROM security_audit_log a
 JOIN security_alert_outbox o ON o.audit_id=a.id WHERE a.id=?`, result.AuditID).Scan(&auditID, &alertStatus); err != nil || auditID != result.AuditID || alertStatus != "pending" {
		t.Fatalf("rollback/reapply lost recovery evidence or alert: %s %s %v", auditID, alertStatus, err)
	}
	_, err = store.ExecContext(ctx, "DELETE FROM security_audit_log WHERE id=?", result.AuditID)
	var sqlErr *mysql.MySQLError
	if !errors.As(err, &sqlErr) || sqlErr.Number != 1644 {
		t.Fatalf("rollback/reapply lost append-only trigger: %v", err)
	}
}
