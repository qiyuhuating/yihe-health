package handlers

import (
	"context"
	"errors"
	"net/http"

	"yihe-care/internal/auth"
	"yihe-care/internal/blob"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/familyrepo"
	"yihe-care/internal/familyworkflow"
	"yihe-care/internal/response"
)

// POST /api/v1/family/updateTwo: update a family profile under its live scope.
func (h *Handler) familyUpdateTwo(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	phone := str(m, "familyPhone")
	name, ok := vstrRequired(w, m, "familyName", 50)
	if !ok {
		return
	}
	sex, ok := vstrRequired(w, m, "familySex", 1)
	if !ok {
		return
	}
	addr, ok := vstrRequired(w, m, "familyAddress", 200)
	if !ok {
		return
	}
	id, okID := int64Val(m, "familyId")
	if phone == "" || name == "" || sex == "" || addr == "" || !okID {
		response.Fail(w, "参数不完整")
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	changed, phoneChanged, err := familyworkflow.UpdateProfile(r.Context(), h.DB, h.Box, *p, id, phone, name, sex, addr)
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		scopeDeny(w)
		return
	case errors.Is(err, familyrepo.ErrPhoneInUse):
		conflict409(w, "手机号已被使用")
		return
	case errors.Is(err, familyrepo.ErrFamilyNotFound):
		recordNotFound(w)
		return
	case internalError(w, err):
		return
	}
	if phoneChanged {
		if err := h.Sessions.RevokeContext(context.WithoutCancel(r.Context()), auth.RoleFamily, id); err != nil {
			response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "家属资料已更新，但会话撤销确认失败"})
			return
		}
	}
	if changed {
		writeSingleMutation(w, 1)
		return
	}
	writeSingleMutation(w, 0)
}

// POST /api/v1/family/updateTouPic: update an owned family avatar.
func (h *Handler) familyUpdateTouPic(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	phone := str(m, "familyPhone")
	pic := str(m, "familyTouPic")
	if phone == "" || pic == "" {
		response.Fail(w, "参数不完整")
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	manager, err := h.images()
	if internalError(w, err) {
		return
	}
	changed, err := familyworkflow.ChangeAvatar(r.Context(), h.DB, h.Box, *p, phone, pic, manager)
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		scopeDeny(w)
	case errors.Is(err, familyrepo.ErrFamilyNotFound):
		recordNotFound(w)
	case errors.Is(err, blob.ErrNotFound):
		write400(w, "图片不存在或已回收，请重新上传")
	case internalError(w, err):
		return
	case !changed:
		recordNotFound(w)
	default:
		response.OK(w, nil)
	}
}
