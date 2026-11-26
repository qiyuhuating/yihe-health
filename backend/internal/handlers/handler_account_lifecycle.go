package handlers

import (
	"context"
	"errors"
	"net/http"

	"yihe-care/internal/auth"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
)

// POST /api/v1/care/accounts/status is the sole HTTP lifecycle entry point.
// Retired accounts are audit tombstones; this endpoint does not erase history.
func (h *Handler) careAccountStatus(w http.ResponseWriter, r *http.Request) {
	actorID, ok := careAdmin(w, r)
	if !ok {
		return
	}
	var req struct {
		AdminID int64  `json:"adminId"`
		Status  string `json:"status"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.AdminID < 1 || (req.Status != "active" && req.Status != "suspended" && req.Status != "retired") {
		write400(w, "管理员或账户状态无效")
		return
	}
	err := careworkflow.ChangeAccountStatus(r.Context(), h.DB, actorID, req.AdminID, req.Status)
	switch {
	case errors.Is(err, careworkflow.ErrAccountMissing):
		response.JSON(w, http.StatusNotFound, map[string]any{"message": "管理员账号不存在"})
		return
	case errors.Is(err, carerepo.ErrLastSupervisor):
		conflict409(w, "必须保留至少一名有效主管")
		return
	case errors.Is(err, careworkflow.ErrAccountRetired):
		conflict409(w, "已注销账号不能恢复")
		return
	case errors.Is(err, careworkflow.ErrAccountSelfChange):
		write400(w, "主管不能停用或注销自身账号")
		return
	case errors.Is(err, careworkflow.ErrDutyDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	case internalError(w, err):
		return
	}
	if err := h.Sessions.RevokeContext(context.WithoutCancel(r.Context()), auth.RoleAdmin, req.AdminID); err != nil {
		response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "账号状态已提交，但会话撤销确认失败"})
		return
	}
	response.OK(w, map[string]any{"adminId": req.AdminID, "status": req.Status})
}
