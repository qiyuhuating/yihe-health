package handlers

import (
	"context"
	"errors"
	"net/http"

	"yihe-care/internal/auth"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/familyrepo"
	"yihe-care/internal/familyworkflow"
	"yihe-care/internal/response"
)

// POST /api/v1/family/updatePassword: verify the old secret and revoke all
// family sessions inside the same account-scoped transaction.
func (h *Handler) familyUpdatePassword(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	phone, _ := m["familyPhone"].(string)
	oldPw, _ := m["oldPassword"].(string)
	newPw, _ := m["familyPassword"].(string)
	if phone == "" || oldPw == "" || newPw == "" {
		response.Fail(w, "请完整填写手机号、原密码和新密码")
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	if !auth.ValidNewPassword(newPw) {
		response.Fail(w, "请填写原密码，新密码须为12–20位且包含字母和数字")
		return
	}
	id, err := familyworkflow.ChangePassword(r.Context(), h.DB, h.Box, *p, phone, oldPw, newPw)
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		scopeDeny(w)
		return
	case errors.Is(err, familyworkflow.ErrWrongPassword), errors.Is(err, familyrepo.ErrFamilyNotFound):
		response.Fail(w, "手机号或原密码不正确")
		return
	case errors.Is(err, familyworkflow.ErrPasswordChanged):
		conflict409(w, "密码已被其他请求修改，请重新验证")
		return
	case internalError(w, err):
		return
	}
	if err := h.Sessions.RevokeContext(context.WithoutCancel(r.Context()), auth.RoleFamily, id); err != nil {
		response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "密码已修改，但会话撤销确认失败"})
		return
	}
	response.OK(w, nil)
}
