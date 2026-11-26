package handlers

import (
	"context"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestContinuousBindingCodeExpiry(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	principal := session.Principal{Role: auth.RoleAdmin, ID: 11}
	createCode := withHealthPrincipal(h, principal, h.deviceCreateBindCode)
	unbind := withHealthPrincipal(h, principal, h.deviceUnbind)
	for _, bad := range []any{"abc", -1, 0, 31, 1.5, true} {
		out := callAPI(createCode, map[string]any{"oldId": 1, "expireDays": bad})
		if out.Code != 400 {
			t.Errorf("invalid expiry %v accepted: HTTP %d", bad, out.Code)
		}
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM bind_code").Scan(&count); err != nil || count != 0 {
		t.Errorf("invalid expiry created codes: count=%d err=%v", count, err)
	}
	api(t, unbind, map[string]any{"deviceId": 1})
	for _, body := range []map[string]any{{"oldId": 1}, {"oldId": 1, "expireDays": ""}} {
		created := api(t, createCode, body)
		var expiry, issued time.Time
		if err := pool.QueryRowContext(context.Background(), "SELECT expire_time,create_time FROM bind_code WHERE code=?", created["code"]).Scan(&expiry, &issued); err != nil {
			t.Fatal(err)
		}
		if validity := expiry.Sub(issued); validity < 23*time.Hour || validity > 25*time.Hour {
			t.Errorf("default code not valid for one day: %v", expiry)
		}
		if bound := api(t, h.deviceBind, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "code": created["code"]}); bound["returnCode"] != "1111" {
			t.Errorf("default code cannot bind device: %v", bound)
		}
		api(t, unbind, map[string]any{"deviceId": 1})
	}
}

func TestContinuousInviteCodeExpiry(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	createInvite := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.inviteCreate)
	for _, bad := range []any{"abc", -1, 1.5, true, "999999999999999999999999"} {
		out := callAPI(createInvite, map[string]any{"oldId": 1, "expireDays": bad})
		if out.Code != 400 {
			t.Errorf("invalid expiry %v became an indefinite invite: HTTP %d", bad, out.Code)
		}
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM invite_code").Scan(&count); err != nil || count != 0 {
		t.Errorf("invalid expiry created invites: count=%d err=%v", count, err)
	}
	for _, valid := range []any{"", nil, 0, "2"} {
		api(t, createInvite, map[string]any{"oldId": 1, "expireDays": valid})
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM invite_code WHERE expire_time IS NULL").Scan(&count); err != nil || count != 3 {
		t.Fatalf("legacy indefinite expiry contract changed: count=%d err=%v", count, err)
	}
}
