package handlers

import (
	"testing"
	"time"
)

func TestCapabilityIdleThenActiveCommands(t *testing.T) {
	h := commandSetup(t)
	ctx := t.Context()
	if err := h.CapabilityTick(ctx); err != nil {
		t.Fatal(err)
	}
	create := capabilityAdmin(h, h.commandCreate)
	id := api(t, create, map[string]any{"deviceId": 1, "action": "location", "requestKey": "idle-expiry", "ttlSeconds": 300})["commandId"]
	mustExec(t, h.DB, "UPDATE device_command SET expires_at=? WHERE id=?", time.Now().Add(-time.Minute), id)
	if err := h.CapabilityTick(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := h.DB.QueryRowContext(ctx, "SELECT status FROM device_command WHERE id=?", id).Scan(&state); err != nil || state != "timeout" {
		t.Fatalf("newly active command was not expired: state=%s err=%v", state, err)
	}
	if err := h.CapabilityTick(ctx); err != nil {
		t.Fatal(err)
	}
}
