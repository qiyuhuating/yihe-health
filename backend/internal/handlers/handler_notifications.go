package handlers

import (
	"errors"
	"net/http"

	"yihe-care/internal/auth"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
)

func (h *Handler) notifications(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	var req struct {
		After    int64 `json:"after"`
		FamilyID int64 `json:"familyId"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if p.Role == auth.RoleFamily {
		req.FamilyID = p.ID
	}
	if req.FamilyID < 1 || req.After < 0 {
		write400(w, "消息范围或游标无效")
		return
	}
	items, err := careworkflow.ListNotifications(r.Context(), h.DB, p.Role, p.ID, req.FamilyID, req.After)
	if errors.Is(err, careworkflow.ErrCapabilityDenied) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "无消息读取权限"})
		return
	}
	if internalError(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"items": items})
}
