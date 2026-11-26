package careworkflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func seedHandoverStaff(t *testing.T) (*db.Database, context.Context) {
	t.Helper()
	store := dbtest.MySQL(t)
	ctx := context.Background()
	for _, query := range []string{
		"INSERT INTO users(user_id,user_name,role) VALUES(81,'care81','1'),(82,'care82','1'),(83,'audit83','1'),(84,'care84','1')",
		"INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(81,'caregiver',81),(82,'caregiver',81),(83,'auditor',81),(84,'caregiver',81)",
	} {
		if _, err := store.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	return store, ctx
}

func insertHandoverIncident(t *testing.T, store *db.Database, ctx context.Context, owner string) int64 {
	t.Helper()
	result, err := store.ExecContext(ctx, `
 INSERT INTO care_incident(old_id,device_id,state,severity,acknowledged_by,acknowledged_at,due_at)
 VALUES(900,901,'in_progress','high',?,CURRENT_TIMESTAMP(6),TIMESTAMPADD(MINUTE,1,CURRENT_TIMESTAMP(6)))`, owner)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestShiftHandoverTransfersOnlySnapshottedIncidents(t *testing.T) {
	store, ctx := seedHandoverStaff(t)

	firstID := insertHandoverIncident(t, store, ctx, "81")
	handoverID, snapshotted, err := CreateShiftHandover(ctx, store, 81, 82, "交接晚班")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshotted) != 1 || snapshotted[0] != firstID {
		t.Fatalf("unexpected handover snapshot: %#v", snapshotted)
	}
	retryID, retryItems, err := CreateShiftHandover(ctx, store, 81, 82, "重复请求")
	if err != nil || retryID != handoverID || len(retryItems) != 1 || retryItems[0] != firstID {
		t.Fatalf("same-target retry was not idempotent: id=%d items=%v err=%v", retryID, retryItems, err)
	}

	secondID := insertHandoverIncident(t, store, ctx, "81")
	transferred, err := AcceptShiftHandover(ctx, store, handoverID, 82)
	if err != nil {
		t.Fatal(err)
	}
	if transferred != 1 {
		t.Fatalf("transferred=%d, want 1", transferred)
	}

	var firstOwner, secondOwner string
	var firstDue time.Time
	if err := store.QueryRowContext(ctx, "SELECT acknowledged_by,due_at FROM care_incident WHERE id=?", firstID).Scan(&firstOwner, &firstDue); err != nil {
		t.Fatal(err)
	}
	if err := store.QueryRowContext(ctx, "SELECT acknowledged_by FROM care_incident WHERE id=?", secondID).Scan(&secondOwner); err != nil {
		t.Fatal(err)
	}
	if firstOwner != "82" || secondOwner != "81" {
		t.Fatalf("handover escaped its snapshot: first=%q second=%q", firstOwner, secondOwner)
	}
	if firstDue.Before(time.Now().Add(9 * time.Minute)) {
		t.Fatalf("accepted handover did not grant a bounded handling window: %v", firstDue)
	}

	if _, _, err := CreateShiftHandover(ctx, store, 81, 83, "审计员不能接班"); !errors.Is(err, ErrHandoverDenied) {
		t.Fatalf("auditor target could accept operational work: %v", err)
	}
}

func TestShiftHandoverCanBeCancelledAndReplaced(t *testing.T) {
	store, ctx := seedHandoverStaff(t)
	incidentID := insertHandoverIncident(t, store, ctx, "81")

	handoverID, _, err := CreateShiftHandover(ctx, store, 81, 82, "原交接")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := CreateShiftHandover(ctx, store, 81, 84, "改交给其他人"); !errors.Is(err, ErrHandoverPending) {
		t.Fatalf("different-target replacement bypassed pending handover: %v", err)
	}
	if err := CancelShiftHandover(ctx, store, handoverID, 82); !errors.Is(err, ErrHandoverState) {
		t.Fatalf("target could cancel source handover: %v", err)
	}
	if _, err := store.ExecContext(ctx, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=82"); err != nil {
		t.Fatal(err)
	}
	recoveredID, recoveredItems, err := CreateShiftHandover(ctx, store, 81, 82, "恢复丢失响应")
	if err != nil || recoveredID != handoverID || len(recoveredItems) != 1 || recoveredItems[0] != incidentID {
		t.Fatalf("pending handover could not be recovered after target demotion: id=%d items=%v err=%v", recoveredID, recoveredItems, err)
	}
	if err := CancelShiftHandover(ctx, store, handoverID, 81); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, "UPDATE care_staff_permission SET duty='caregiver' WHERE admin_id=82"); err != nil {
		t.Fatal(err)
	}
	if _, err := AcceptShiftHandover(ctx, store, handoverID, 82); !errors.Is(err, ErrHandoverState) {
		t.Fatalf("cancelled handover was accepted: %v", err)
	}

	replacementID, items, err := CreateShiftHandover(ctx, store, 81, 84, "改交给其他人")
	if err != nil {
		t.Fatal(err)
	}
	if replacementID == handoverID || len(items) != 1 || items[0] != incidentID {
		t.Fatalf("replacement handover mismatch: id=%d old=%d items=%v", replacementID, handoverID, items)
	}
}
