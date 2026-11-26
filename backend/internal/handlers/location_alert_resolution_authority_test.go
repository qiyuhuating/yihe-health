package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
	"yihe-care/internal/telemetryrepo"
)

func TestLocationAlertResolutionRequiresLiveHandleAlertsDuty(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 71, "supervisor")
	seedCareAdminDuty(t, pool, 72, "caregiver")
	h := testHandler(t, pool)
	ctx := context.Background()

	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	alarmID, err := telemetryrepo.InsertAlarm(ctx, tx, telemetryrepo.Alarm{
		ResidentID: 1, DeviceID: 1, ObservedAt: time.Now(), Kind: "fall",
	})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"oldId": 1}
	for _, principal := range []session.Principal{
		{Role: auth.RoleAdmin, ID: 72},
		{Role: auth.RoleFamily, ID: 1, OldID: 1},
	} {
		got := callAPI(withHealthPrincipal(h, principal, h.resolveLatestLocationAlert), body)
		if got.Code != http.StatusForbidden {
			t.Fatalf("unauthorized actor %s/%d resolved alert: HTTP %d %s",
				principal.Role, principal.ID, got.Code, got.Body.String())
		}
	}
	var status string
	if err := pool.QueryRowContext(ctx, "SELECT status FROM alarm_precess WHERE id=?", alarmID).Scan(&status); err != nil || status != "1" {
		t.Fatalf("unauthorized resolution changed alert: status=%s err=%v", status, err)
	}

	supervisor := session.Principal{Role: auth.RoleAdmin, ID: 71}
	got := callAPI(withHealthPrincipal(h, supervisor, h.resolveLatestLocationAlert), body)
	if got.Code != http.StatusOK {
		t.Fatalf("supervisor could not resolve alert: HTTP %d %s", got.Code, got.Body.String())
	}
	if err := pool.QueryRowContext(ctx, "SELECT status FROM alarm_precess WHERE id=?", alarmID).Scan(&status); err != nil || status != "0" {
		t.Fatalf("authorized resolution did not commit: status=%s err=%v", status, err)
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='auditor' WHERE admin_id=71")
	got = callAPI(withHealthPrincipal(h, supervisor, h.resolveLatestLocationAlert), body)
	if got.Code != http.StatusForbidden {
		t.Fatalf("revoked supervisor could still resolve alerts: HTTP %d %s", got.Code, got.Body.String())
	}
}
