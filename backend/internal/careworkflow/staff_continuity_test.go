package careworkflow

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"yihe-care/internal/carerepo"
	"yihe-care/internal/db/dbtest"
)

func TestConcurrentDutyReassignmentsPreserveSupervisor(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	for _, query := range []string{
		"INSERT INTO users(user_id,user_name,role) VALUES(71,'sup71','1'),(72,'sup72','1')",
		"INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(71,'supervisor',71),(72,'supervisor',72)",
	} {
		if _, err := pool.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 2)
	go func() { results <- AssignDuty(ctx, pool, 71, 72, "caregiver") }()
	go func() { results <- AssignDuty(ctx, pool, 72, 71, "caregiver") }()
	success, denied := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, ErrDutyDenied):
			denied++
		default:
			t.Fatalf("unexpected concurrent assignment failure: %v", err)
		}
	}
	if success != 1 || denied != 1 {
		t.Fatalf("expected one assignment and one revoked actor, success=%d denied=%d", success, denied)
	}
	var remainingID int64
	if err := pool.QueryRowContext(ctx, `SELECT p.admin_id FROM care_staff_permission p
 JOIN users u ON u.user_id=p.admin_id AND u.role='1'
 WHERE p.duty='supervisor'`).Scan(&remainingID); err != nil {
		t.Fatalf("supervisor disappeared: %v", err)
	}
	removedID := int64(143) - remainingID
	if err := AssignDuty(ctx, pool, remainingID, removedID, "unassigned"); err != nil {
		t.Fatalf("supervisor could not revoke a caregiver assignment: %v", err)
	}
	var assignments int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_staff_permission WHERE admin_id=?", removedID).Scan(&assignments); err != nil || assignments != 0 {
		t.Fatalf("unassigned caregiver retained a permission row: count=%d err=%v", assignments, err)
	}
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := carerepo.LockAuthority(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := carerepo.SetDuty(ctx, tx, remainingID, remainingID, "unassigned"); !errors.Is(err, carerepo.ErrLastSupervisor) {
		t.Fatalf("last supervisor could be removed: %v", err)
	}
}
