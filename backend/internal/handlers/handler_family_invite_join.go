package handlers

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/inviterepo"
	"yihe-care/internal/inviteworkflow"
	"yihe-care/internal/response"
)

func (h *Handler) inviteOldPersonList(w http.ResponseWriter, r *http.Request) {
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadInvites)
	if !ok {
		return
	}
	defer tx.Rollback()
	list, err := inviterepo.ListResidents(r.Context(), tx)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"oldPeople": list})
}

// POST /api/v1/inviteCode/checkCode —— 校验邀请码（公开）。
func (h *Handler) inviteCheckCode(w http.ResponseWriter, r *http.Request) {
	if decision := h.Risk.BeforeInvite(h.clientIP(r)); !decision.Allowed() {
		h.Risk.RecordDenied(h.clientIP(r), "", "invite_restricted", r.UserAgent(),
			r.Header.Get("Sec-CH-UA-Model"), r.Header.Get("X-App-Device-ID"))
		h.riskDeniedInvite(w)
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	code := normalizeCode(str(m, "code"))
	if code == "" {
		response.Fail(w, "请输入邀请码")
		return
	}
	status, err := inviterepo.ReadCode(r.Context(), h.DB, code)
	if err != nil {
		if isNotFound(err) {
			h.Risk.InvalidInvite(h.clientIP(r), r.UserAgent(), r.Header.Get("Sec-CH-UA-Model"), r.Header.Get("X-App-Device-ID"))
			response.Error(w, http.StatusNotFound, "INVITE_NOT_FOUND", "邀请码不存在，请向管理员确认")
			return
		}
		response.Error500(w, ref())
		return
	}
	switch inviteStatus(status.Used, status.ExpireTime, time.Now()) {
	case "used":
		response.Error(w, http.StatusConflict, "INVITE_USED", "该邀请码已被使用")
		return
	case "expired":
		response.Error(w, http.StatusConflict, "INVITE_EXPIRED", "该邀请码已过期，请向管理员重新获取")
		return
	}
	nm := ""
	if status.ResidentName != nil {
		nm = *status.ResidentName
	}
	response.OK(w, map[string]any{
		"oldPersonName": nm, "expireTime": fmtTS(status.ExpireTime), "remainDays": inviteRemainingDays(status.ExpireTime, time.Now()),
	})
}

// POST /api/v1/inviteCode/join —— 家属凭邀请码注册（公开，事务）。
func (h *Handler) inviteJoin(w http.ResponseWriter, r *http.Request) {
	if decision := h.Risk.BeforeInvite(h.clientIP(r)); !decision.Allowed() {
		h.Risk.RecordDenied(h.clientIP(r), "", "invite_restricted", r.UserAgent(),
			r.Header.Get("Sec-CH-UA-Model"), r.Header.Get("X-App-Device-ID"))
		h.riskDeniedInvite(w)
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	registration := parseInvitationRegistration(m)
	if message := registration.validationMessage(); message != "" {
		response.Fail(w, message)
		return
	}
	result, failure, err := inviteworkflow.Join(r.Context(), h.DB, h.Box, inviteworkflow.Registration{
		Code: registration.code, Name: registration.familyName,
		Phone: registration.familyPhone, Password: registration.familyPassword,
		Relation: registration.familyRelation, Sex: registration.familySex,
		Address: registration.familyAddress,
	})
	if err != nil {
		response.Error500(w, ref())
		return
	}
	switch failure {
	case inviteworkflow.CodeMissing:
		response.Fail(w, "邀请码不存在，请向管理员确认")
	case inviteworkflow.CodeUsed:
		response.Fail(w, "该邀请码已被使用")
	case inviteworkflow.CodeExpired:
		response.Fail(w, "该邀请码已过期，请向管理员重新获取")
	case inviteworkflow.ResidentMissing:
		response.Fail(w, "邀请码绑定的老人不存在，请联系管理员")
	case inviteworkflow.FamilyMismatch:
		response.Fail(w, "注册失败，请核对邀请码与信息后重试")
	case inviteworkflow.RegistrationLost:
		conflict409(w, "注册失败，请重试")
	case inviteworkflow.CodeClaimed:
		conflict409(w, "邀请码已失效，请重新获取")
	default:
		response.OK(w, map[string]any{
			"familyId": result.FamilyID, "familyName": result.Name, "oldPersonName": result.Resident,
		})
	}
}

type invitationRegistration struct {
	code           string
	familyName     string
	familyPhone    string
	familyPassword string
	familyRelation string
	familySex      string
	familyAddress  string
}

func parseInvitationRegistration(m map[string]any) invitationRegistration {
	return invitationRegistration{
		code:           normalizeCode(str(m, "code")),
		familyName:     str(m, "familyName"),
		familyPhone:    str(m, "familyPhone"),
		familyPassword: str(m, "familyPassword"),
		familyRelation: str(m, "familyRelation"),
		familySex:      str(m, "familySex"),
		familyAddress:  str(m, "familyAddress"),
	}
}

func (registration *invitationRegistration) validationMessage() string {
	if registration.code == "" {
		return "请输入邀请码"
	}
	if strings.TrimSpace(registration.familyName) == "" {
		return "请输入姓名"
	}
	if utf8.RuneCountInString(registration.familyName) > 50 {
		return "姓名长度不能超过50个字符"
	}
	if !auth.ValidPhone(registration.familyPhone) {
		return "请输入正确的11位手机号"
	}
	if !auth.ValidNewPassword(registration.familyPassword) {
		return "密码须为12–20位且包含字母和数字"
	}
	if utf8.RuneCountInString(registration.familyRelation) > 10 {
		return "与老人关系长度不能超过10个字符"
	}
	if registration.familySex != "男" && registration.familySex != "女" {
		return "请选择性别"
	}
	if utf8.RuneCountInString(registration.familyAddress) > 200 {
		return "地址长度不能超过200个字符"
	}

	return ""
}
