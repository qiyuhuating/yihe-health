package risk_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/risk"
)

func riskStore(t *testing.T) (*db.Database, *risk.Manager) {
	t.Helper()
	pool := dbtest.MySQL(t)
	t.Setenv("YIHE_RISK_AUDIT_SPOOL_PATH", filepath.Join(t.TempDir(), "audit.jsonl"))
	m := risk.New()
	if err := m.EnableMySQL(pool.DB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if dropped := m.CloseMySQL(); dropped != 0 {
			t.Errorf("dropped %d events", dropped)
		}
	})
	riskExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(1,'actor','1'),(2,'supervisor','1')")
	riskExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'supervisor',2),(2,'supervisor',2)")
	return pool, m
}

func riskExec(t *testing.T, pool *db.Database, query string, args ...any) {
	t.Helper()
	if _, err := pool.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func riskCounts(t *testing.T, pool *db.Database, rules, events int) {
	t.Helper()
	var r, e int
	if err := pool.QueryRowContext(context.Background(), "SELECT (SELECT COUNT(*) FROM risk_ip_rules),(SELECT COUNT(*) FROM risk_login_events)").Scan(&r, &e); err != nil {
		t.Fatal(err)
	}
	if r != rules || e != events {
		t.Fatalf("rules/audits=%d/%d want %d/%d", r, e, rules, events)
	}
}

func authorized(t *testing.T, pool *db.Database) *sql.Tx {
	t.Helper()
	tx, _, err := careworkflow.BeginAuthorized(context.Background(), pool, 1, carepolicy.ManageRisk)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

func insertRule(t *testing.T, m *risk.Manager, tx *sql.Tx) risk.Rule {
	t.Helper()
	rule, err := m.InsertRuleTx(context.Background(), tx, "203.0.113.9", "block", "regression", "admin:1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestMySQLRiskRuleTransactionRollbackAndReplicas(t *testing.T) {
	pool, m := riskStore(t)
	if _, err := m.AddRule("203.0.113.9", "block", "regression", "admin:1", 0); err == nil {
		t.Fatal("nontransactional MySQL create accepted")
	}
	if _, err := m.DeleteRule("missing", "admin:1"); err == nil {
		t.Fatal("nontransactional MySQL delete accepted")
	}
	replica := risk.New()
	if err := replica.EnableMySQL(pool.DB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { replica.CloseMySQL() })
	tx := authorized(t, pool)
	insertRule(t, m, tx)
	if !m.BeforeLogin("203.0.113.9", "account").Allowed() {
		t.Fatal("uncommitted rule published")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	riskCounts(t, pool, 0, 0)
	tx = authorized(t, pool)
	rule := insertRule(t, m, tx)
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	riskCounts(t, pool, 1, 1)
	if replica.BeforeLogin("203.0.113.9", "account").Allowed() || replica.BeforeInvite("203.0.113.9").Allowed() {
		t.Fatal("replica missed committed block")
	}
	_, rules, err := replica.HistoryMySQL(context.Background(), 10)
	if err != nil || len(rules) != 1 {
		t.Fatalf("replica history stale: %v", err)
	}
	tx = authorized(t, pool)
	if ok, err := m.RemoveRuleTx(context.Background(), tx, rule.ID, "admin:1"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	tx.Rollback()
	riskCounts(t, pool, 1, 1)
	tx = authorized(t, pool)
	if ok, err := m.RemoveRuleTx(context.Background(), tx, rule.ID, "admin:1"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	riskCounts(t, pool, 0, 2)
	if !replica.BeforeLogin("203.0.113.9", "account").Allowed() {
		t.Fatal("replica retained deleted block")
	}
}

func TestMySQLRiskAuditFailureAndCommitFailure(t *testing.T) {
	pool, m := riskStore(t)
	riskExec(t, pool, "CREATE TRIGGER reject_risk_audit BEFORE INSERT ON risk_login_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected audit failure'")
	tx := authorized(t, pool)
	if _, err := m.InsertRuleTx(context.Background(), tx, "203.0.113.9", "block", "regression", "admin:1", 0); err == nil {
		t.Fatal("audit failure ignored")
	}
	tx.Rollback()
	riskCounts(t, pool, 0, 0)
	riskExec(t, pool, "DROP TRIGGER reject_risk_audit")
	tx = authorized(t, pool)
	insertRule(t, m, tx)
	var connection int
	if err := tx.QueryRow("SELECT CONNECTION_ID()").Scan(&connection); err != nil {
		t.Fatal(err)
	}
	riskExec(t, pool, fmt.Sprintf("KILL CONNECTION %d", connection))
	if err := tx.Commit(); err == nil {
		t.Fatal("killed connection committed")
	}
	riskCounts(t, pool, 0, 0)
	if !m.BeforeLogin("203.0.113.9", "account").Allowed() {
		t.Fatal("failed commit published rule")
	}
	// Closing only this test's pool simulates storage unavailability.
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := careworkflow.BeginAuthorized(context.Background(), pool, 1, carepolicy.ManageRisk); err == nil {
		t.Fatal("unavailable DB authorized")
	}
	if m.BeforeLogin("203.0.113.9", "account").Allowed() || m.BeforeInvite("203.0.113.9").Allowed() {
		t.Fatal("unavailable storage bypassed rules")
	}
}

func TestMySQLRiskMutationSerializesDutyRevocation(t *testing.T) {
	pool, m := riskStore(t)
	tx := authorized(t, pool)
	insertRule(t, m, tx)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- careworkflow.AssignDuty(ctx, pool, 2, 1, "caregiver") }()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("revoke passed active rule transaction: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := careworkflow.AssignDuty(context.Background(), pool, 2, 1, "caregiver"); err != nil {
		t.Fatal(err)
	}
	if tx, _, err := careworkflow.BeginAuthorized(context.Background(), pool, 1, carepolicy.ManageRisk); !errors.Is(err, careworkflow.ErrCapabilityDenied) {
		if tx != nil {
			tx.Rollback()
		}
		t.Fatalf("revoked admin authorized: %v", err)
	}
	riskCounts(t, pool, 1, 1)
	// Revoke holds the lock first: the waiting mutation sees the new duty.
	riskExec(t, pool, "UPDATE care_staff_permission SET duty='supervisor' WHERE admin_id=1")
	revoke, err := pool.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer revoke.Rollback()
	if _, err := revoke.Exec("UPDATE care_staff_permission SET duty='caregiver' WHERE admin_id=1"); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	go func() {
		close(started)
		tx, _, err := careworkflow.BeginAuthorized(context.Background(), pool, 1, carepolicy.ManageRisk)
		if tx != nil {
			tx.Rollback()
		}
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("mutation bypassed revoke lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := revoke.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, careworkflow.ErrCapabilityDenied) {
		t.Fatal(err)
	}
	riskCounts(t, pool, 1, 1)
}

func TestMySQLRiskRejectsInactiveActorsAndConcurrentWrites(t *testing.T) {
	pool, m := riskStore(t)
	for _, change := range []string{"UPDATE users SET account_status='suspended' WHERE user_id=1", "UPDATE users SET role='0' WHERE user_id=1", "DELETE FROM care_staff_permission WHERE admin_id=1"} {
		tx, err := pool.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(change); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		denied, _, err := careworkflow.BeginAuthorized(context.Background(), pool, 1, carepolicy.ManageRisk)
		if denied != nil {
			denied.Rollback()
		}
		if !errors.Is(err, careworkflow.ErrCapabilityDenied) {
			t.Fatalf("inactive actor authorized: %v", err)
		}
		riskExec(t, pool, "UPDATE users SET account_status='active',role='1' WHERE user_id=1")
	}
	riskExec(t, pool, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'supervisor',2)")
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, _, err := careworkflow.BeginAuthorized(context.Background(), pool, 1, carepolicy.ManageRisk)
			if err != nil {
				failures <- err
				return
			}
			defer tx.Rollback()
			_, err = m.InsertRuleTx(context.Background(), tx, "203.0.113.9", "block", "concurrent", "admin:1", 0)
			if err == nil {
				err = tx.Commit()
			}
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	riskCounts(t, pool, 8, 8)
}
