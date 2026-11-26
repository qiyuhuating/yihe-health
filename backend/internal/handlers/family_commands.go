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

// familyFields validates the common administrative family profile.
func familyFields(w http.ResponseWriter, m map[string]any) (familyrepo.Family, bool) {
	name, okName := vstrRequired(w, m, "familyName", 50)
	if !okName {
		return familyrepo.Family{}, false
	}
	relation, okRelation := vstrRequired(w, m, "familyRelation", 10)
	if !okRelation {
		return familyrepo.Family{}, false
	}
	sex, okSex := vstrRequired(w, m, "familySex", 1)
	if !okSex {
		return familyrepo.Family{}, false
	}
	phone, okPhone := vstrRequired(w, m, "familyPhone", 12)
	if !okPhone {
		return familyrepo.Family{}, false
	}
	address, okAddress := vstrRequired(w, m, "familyAddress", 200)
	if !okAddress {
		return familyrepo.Family{}, false
	}
	family := familyrepo.Family{
		Name: name, Relation: relation, Sex: sex, Phone: phone, Address: address,
	}
	if !auth.ValidPhone(family.Phone) {
		response.Fail(w, "请输入正确的11位手机号")
		return familyrepo.Family{}, false
	}
	return family, true
}

// POST /api/v1/family/add: create an unactivated family and bind its elder.
func (h *Handler) familyAdd(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	oldPhone := str(m, "oldPhone")
	if oldPhone == "" {
		response.Fail(w, "参数不完整")
		return
	}
	family, ok := familyFields(w, m)
	if !ok {
		return
	}
	err := familyworkflow.Create(r.Context(), h.DB, h.Box, actor, family, oldPhone)
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	case errors.Is(err, familyrepo.ErrPhoneInUse):
		response.Fail(w, "该手机号已被使用，请更换手机号")
	case errors.Is(err, familyrepo.ErrResidentMissing):
		response.Fail(w, "未找到唯一长者档案，请核对老人手机号")
	case errors.Is(err, familyrepo.ErrResidentAmbiguous):
		conflict409(w, "该手机号对应多份长者档案，请通过指定老人的邀请码绑定")
	case errors.Is(err, familyrepo.ErrResidentBound):
		conflict409(w, "该老人已绑定家属，请先处理原绑定")
	case internalError(w, err):
		return
	default:
		response.OK(w, nil)
	}
}

// POST /api/v1/family/updateFamily: update an administrative family record.
func (h *Handler) familyUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, valid := int64Val(m, "familyId")
	if !valid {
		response.Error500(w, ref())
		return
	}
	family, ok := familyFields(w, m)
	if !ok {
		return
	}
	err := familyworkflow.Update(r.Context(), h.DB, h.Box, actor, id, family)
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	case errors.Is(err, familyrepo.ErrPhoneInUse):
		response.Fail(w, "该手机号已被使用，请更换手机号")
	case errors.Is(err, familyrepo.ErrFamilyNotFound):
		recordNotFound(w)
	case internalError(w, err):
		return
	default:
		response.OK(w, nil)
	}
}

// POST /api/v1/family/deleteFamily: remove binding and revoke sessions.
func (h *Handler) familyDelete(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, valid := int64Val(m, "familyId")
	if !valid {
		response.Error500(w, ref())
		return
	}
	err := familyworkflow.Delete(r.Context(), h.DB, actor, id)
	switch {
	case errors.Is(err, careworkflow.ErrCapabilityDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	case errors.Is(err, familyrepo.ErrFamilyNotFound):
		recordNotFound(w)
		return
	case internalError(w, err):
		return
	}
	if err := h.Sessions.RevokeContext(context.WithoutCancel(r.Context()), auth.RoleFamily, id); err != nil {
		response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "账号已删除，但会话撤销确认失败"})
		return
	}
	response.OK(w, nil)
}
