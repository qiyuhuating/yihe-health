package handlers

import (
	"context"
	"testing"
	"time"
)

func TestCommandProfileReplayPreservesDeliveries(t *testing.T) {
	h := commandSetup(t)
	ctx := context.Background()
	create := capabilityAdmin(h, h.commandCreate)
	queue := func(key string) any {
		return api(t, create, map[string]any{"deviceId": 1, "action": "location", "requestKey": key, "ttlSeconds": 300})["commandId"]
	}
	completed := queue("completed")
	delivery := api(t, h.commandPull, commandCredentials())["command"].(map[string]any)
	ack := commandCredentials()
	ack["commandId"], ack["deliveryToken"], ack["result"] = completed, delivery["deliveryToken"], "executed"
	api(t, h.commandAck, ack)
	sent := queue("sent")
	delivery = api(t, h.commandPull, commandCredentials())["command"].(map[string]any)
	pending := queue("pending")
	var updated time.Time
	if err := h.DB.QueryRowContext(ctx, "SELECT updated_at FROM device_command_profile WHERE device_id=1").Scan(&updated); err != nil {
		t.Fatal(err)
	}
	profile := map[string]any{"deviceId": 1, "wireType": "K1", "protocol": "yihe-pull-v1", "enabled": true}
	api(t, capabilityAdmin(h, h.commandProfile), profile)
	var state, token string
	var attempts int
	if err := h.DB.QueryRowContext(ctx, "SELECT status,delivery_token,attempts FROM device_command WHERE id=?", sent).Scan(&state, &token, &attempts); err != nil || state != "sent" || token != delivery["deliveryToken"] || attempts != 1 {
		t.Fatalf("profile replay changed in-flight delivery: state=%s attempts=%d err=%v", state, attempts, err)
	}
	if err := h.DB.QueryRowContext(ctx, "SELECT status FROM device_command WHERE id=?", pending).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("profile replay cancelled pending command: %s %v", state, err)
	}
	var after time.Time
	if err := h.DB.QueryRowContext(ctx, "SELECT updated_at FROM device_command_profile WHERE device_id=1").Scan(&after); err != nil || !after.Equal(updated) {
		t.Fatalf("profile replay rewrote update time: %v", err)
	}
	var audits int
	if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_access_audit WHERE action='command_profile'").Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("profile replay wrote a change audit: %d %v", audits, err)
	}
	profile["enabled"] = false
	api(t, capabilityAdmin(h, h.commandProfile), profile)
	for _, id := range []any{sent, pending} {
		if err := h.DB.QueryRowContext(ctx, "SELECT status FROM device_command WHERE id=?", id).Scan(&state); err != nil || state != "cancelled" {
			t.Fatalf("changed profile retained old command: %s %v", state, err)
		}
	}
	if err := h.DB.QueryRowContext(ctx, "SELECT status FROM device_command WHERE id=?", completed).Scan(&state); err != nil || state != "acked" {
		t.Fatalf("profile change rewrote completed command: %s %v", state, err)
	}
	if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_access_audit WHERE action='command_profile'").Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("profile change missing audit: %d %v", audits, err)
	}
}

func TestCommandProfileChangeRollbackAndConcurrency(t *testing.T) {
	h := commandSetup(t)
	ctx := context.Background()
	id := api(t, capabilityAdmin(h, h.commandCreate), map[string]any{
		"deviceId": 1, "action": "location", "requestKey": "profile-change", "ttlSeconds": 300,
	})["commandId"]
	profile := map[string]any{"deviceId": 1, "wireType": "K1", "protocol": "yihe-pull-v1", "enabled": false}
	configure := capabilityAdmin(h, h.commandProfile)
	mustExec(t, h.DB, `CREATE TRIGGER reject_profile_audit BEFORE INSERT ON care_access_audit
 FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='profile audit unavailable'`)
	if r := callAPI(configure, profile); r.Code != 500 {
		t.Fatalf("audit failure returned HTTP %d", r.Code)
	}
	var enabled bool
	if err := h.DB.QueryRowContext(ctx, "SELECT enabled FROM device_command_profile WHERE device_id=1").Scan(&enabled); err != nil || !enabled {
		t.Fatalf("audit failure committed profile: %v", err)
	}
	var state string
	if err := h.DB.QueryRowContext(ctx, "SELECT status FROM device_command WHERE id=?", id).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("audit failure committed command cancellation: %s %v", state, err)
	}
	mustExec(t, h.DB, "DROP TRIGGER reject_profile_audit")
	results := make(chan int, 2)
	for range 2 {
		go func() { results <- callAPI(configure, profile).Code }()
	}
	for range 2 {
		if code := <-results; code != 200 {
			t.Fatalf("concurrent profile update returned HTTP %d", code)
		}
	}
	var audits int
	if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_access_audit WHERE action='command_profile'").Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("concurrent replay wrote multiple change audits: %d %v", audits, err)
	}
	if err := h.DB.QueryRowContext(ctx, "SELECT enabled FROM device_command_profile WHERE device_id=1").Scan(&enabled); err != nil || enabled {
		t.Fatalf("successful change did not disable profile: %v", err)
	}
	if err := h.DB.QueryRowContext(ctx, "SELECT status FROM device_command WHERE id=?", id).Scan(&state); err != nil || state != "cancelled" {
		t.Fatalf("successful change did not cancel command: %s %v", state, err)
	}
}
