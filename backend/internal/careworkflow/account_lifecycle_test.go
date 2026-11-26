package careworkflow

import (
	"context"
	"errors"
	"sync"
	"testing"

	"yihe-care/internal/carerepo"
	"yihe-care/internal/db/dbtest"
)

func TestConcurrentAdministratorSuspensionPreservesSupervisor(t *testing.T) {
	store := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := store.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role) VALUES(71,'a71','1'),(72,'a72','1')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(71,'supervisor',71),(72,'supervisor',72)"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, args := range [][2]int64{{71, 72}, {72, 71}} {
		wg.Add(1)
		go func(actor, target int64) {
			defer wg.Done()
			outcomes <- ChangeAccountStatus(ctx, store, actor, target, "suspended")
		}(args[0], args[1])
	}
	wg.Wait()
	close(outcomes)
	succeeded, denied := 0, 0
	for err := range outcomes {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrDutyDenied):
			denied++
		default:
			t.Fatalf("unexpected lifecycle result: %v", err)
		}
	}
	if succeeded != 1 || denied != 1 {
		t.Fatalf("expected one suspension and one denied retired actor: success=%d denied=%d", succeeded, denied)
	}
	var active int
	if err := store.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u
 JOIN care_staff_permission p ON p.admin_id=u.user_id
 WHERE u.account_status='active' AND p.duty='supervisor'`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active supervisors=%d, err=%v", active, err)
	}
	var suspendedID int64
	if err := store.QueryRowContext(ctx, "SELECT user_id FROM users WHERE account_status='suspended'").Scan(&suspendedID); err != nil {
		t.Fatal(err)
	}
	if duty, err := carerepo.Duty(ctx, store, suspendedID); err != nil || duty != "" {
		t.Fatalf("suspended administrator retained duty=%q err=%v", duty, err)
	}
	remainingID := int64(143) - suspendedID
	if err := ChangeAccountStatus(ctx, store, remainingID, suspendedID, "active"); err != nil {
		t.Fatalf("supervisor could not reactivate account: %v", err)
	}
	if err := ChangeAccountStatus(ctx, store, remainingID, suspendedID, "retired"); err != nil {
		t.Fatalf("supervisor could not retire another account: %v", err)
	}
	if err := ChangeAccountStatus(ctx, store, remainingID, suspendedID, "active"); !errors.Is(err, ErrAccountRetired) {
		t.Fatalf("retired administrator was reactivated: %v", err)
	}
	if err := ChangeAccountStatus(ctx, store, remainingID, remainingID, "suspended"); !errors.Is(err, ErrAccountSelfChange) {
		t.Fatalf("supervisor could suspend self: %v", err)
	}
}
