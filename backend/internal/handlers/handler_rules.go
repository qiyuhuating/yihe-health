package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/response"
	"yihe-care/internal/rules"
)

func (h *Handler) rulePolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		DeviceID int64         `json:"deviceId"`
		Revision int64         `json:"revision"`
		Policy   *rules.Policy `json:"policy"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.DeviceID < 1 || req.Revision < 0 {
		write400(w, "设备或版本无效")
		return
	}
	ctx := r.Context()
	action := carepolicy.ReadRules
	if req.Policy != nil {
		action = carepolicy.ManageRules
	}
	tx, ok := h.beginAdminActionTx(w, r, action)
	if !ok {
		return
	}
	defer tx.Rollback()
	id := req.DeviceID
	if !lockDeviceID(w, ctx, tx, id) {
		return
	}
	var b []byte
	var revision int64
	err := tx.QueryRowContext(ctx, "SELECT revision,payload FROM rule_policy WHERE device_id=? FOR UPDATE", id).Scan(&revision, &b)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		response.Error500(w, ref())
		return
	}
	if req.Policy == nil {
		policy := rules.Default()
		if len(b) > 0 && json.Unmarshal(b, &policy) != nil {
			response.Error500(w, ref())
			return
		}
		if internalError(w, tx.Commit()) {
			return
		}
		response.OK(w, map[string]any{"policy": policy, "revision": revision})
		return
	}
	if err = req.Policy.Validate(); err != nil {
		write400(w, err.Error())
		return
	}
	if req.Revision != revision {
		conflict409(w, "规则版本已变化")
		return
	}
	b, _ = json.Marshal(req.Policy)
	revision++
	_, err = tx.ExecContext(ctx, `INSERT INTO rule_policy(device_id,revision,payload,updated_by) VALUES(?,?,?,?) AS new ON DUPLICATE KEY UPDATE revision=new.revision,payload=new.payload,updated_by=new.updated_by,updated_at=CURRENT_TIMESTAMP(6)`, id, revision, json.RawMessage(b), actor)
	if err == nil {
		_, err = tx.ExecContext(ctx, "DELETE FROM rule_state WHERE device_id=?", id)
	}
	if err == nil {
		err = commitCareChange(ctx, tx, actor, "rule_policy")
	}
	if internalError(w, err) {
		return
	}
	response.OK(w, map[string]any{"revision": revision})
}
