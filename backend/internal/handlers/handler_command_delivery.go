package handlers

import (
	"database/sql"
	"github.com/google/uuid"
	"net/http"
	"time"
	"yihe-care/internal/response"
)

// Pull claims one command atomically; sent means delivered for execution, not ACK.
func (h *Handler) commandPull(w http.ResponseWriter, r *http.Request) {
	var req capabilityCredentials
	if !decodeCapability(w, r, &req) {
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if internalError(w, err) {
		return
	}
	defer tx.Rollback()
	d := h.capabilityDevice(w, r, tx, req)
	if d == nil {
		return
	}
	var enabled bool
	err = tx.QueryRowContext(ctx, "SELECT enabled FROM device_command_profile WHERE device_id=? AND protocol='yihe-pull-v1'", d.ID).Scan(&enabled)
	if isNotFound(err) || (err == nil && !enabled) {
		conflict409(w, "指令协议未启用")
		return
	}
	if internalError(w, err) {
		return
	}
	var id int64
	var action, wire, channel string
	var expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT id,action,wire_action,channel,expires_at FROM device_command
 WHERE device_id=? AND epoch=? AND old_id=? AND status='pending' AND retry_at<=CURRENT_TIMESTAMP(6) AND expires_at>CURRENT_TIMESTAMP(6)
 ORDER BY id LIMIT 1 FOR UPDATE`, d.ID, d.HealthEpoch, *d.OldID).Scan(&id, &action, &wire, &channel, &expires)
	if isNotFound(err) {
		response.OK(w, map[string]any{"command": nil})
		return
	}
	if internalError(w, err) {
		return
	}
	token := uuid.NewString()
	_, err = tx.ExecContext(ctx, "UPDATE device_command SET status='sent',attempts=attempts+1,delivery_token=?,sent_at=CURRENT_TIMESTAMP(6),retry_at=? WHERE id=?", token, time.Now().UTC().Add(30*time.Second), id)
	if err == nil {
		err = tx.Commit()
	}
	if internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"command": map[string]any{"commandId": id, "deliveryToken": token, "action": action, "wireAction": wire, "channel": channel, "expiresAt": expires, "bindingVersion": d.HealthEpoch}})
}

func (h *Handler) commandAck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		capabilityCredentials
		CommandID     int64  `json:"commandId"`
		DeliveryToken string `json:"deliveryToken"`
		Result        string `json:"result"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.CommandID < 1 || req.DeliveryToken == "" || (req.Result != "executed" && req.Result != "failed") {
		write400(w, "回执参数无效")
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if internalError(w, err) {
		return
	}
	defer tx.Rollback()
	d := h.capabilityDevice(w, r, tx, req.capabilityCredentials)
	if d == nil {
		return
	}
	var status string
	var token *string
	var expires time.Time
	err = tx.QueryRowContext(ctx, "SELECT status,delivery_token,expires_at FROM device_command WHERE id=? AND device_id=? AND epoch=? AND old_id=? FOR UPDATE", req.CommandID, d.ID, d.HealthEpoch, *d.OldID).Scan(&status, &token, &expires)
	want := "acked"
	if req.Result == "failed" {
		want = "failed"
	}
	if isNotFound(err) {
		conflict409(w, "指令不属于当前绑定")
		return
	}
	if internalError(w, err) {
		return
	}
	if token == nil || *token != req.DeliveryToken || (status != want && (status != "sent" || !time.Now().Before(expires))) {
		conflict409(w, "回执与当前投递不匹配或已超时")
		return
	}
	if status != want {
		_, err = tx.ExecContext(ctx, "UPDATE device_command SET status=?,acked_at=CURRENT_TIMESTAMP(6) WHERE id=?", want, req.CommandID)
	}
	if err == nil {
		err = tx.Commit()
	}
	if internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"commandId": req.CommandID, "status": want})
}
