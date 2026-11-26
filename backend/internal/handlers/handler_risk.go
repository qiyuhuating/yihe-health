package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/response"
	"yihe-care/internal/risk"
)

func (h *Handler) routeRisk(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/risk/events", h.riskAdminEvents)
	mux.HandleFunc("POST /api/v1/risk/rules", h.riskAdminRuleCreate)
	mux.HandleFunc("DELETE /api/v1/risk/rules/{id}", h.riskAdminRuleDelete)
}

func (h *Handler) riskDeniedLogin(w http.ResponseWriter) {
	response.JSON(w, http.StatusUnauthorized, map[string]any{"code": "INVALID_CREDENTIALS", "message": "手机号或密码不正确"})
}

func (h *Handler) riskDeniedInvite(w http.ResponseWriter) {
	response.Error(w, http.StatusNotFound, "INVITE_NOT_FOUND", "邀请码不存在，请向管理员确认")
}

func (h *Handler) riskAdmin(w http.ResponseWriter, r *http.Request, action carepolicy.Action) (*sql.Tx, string, bool) {
	sess := h.Sessions.FromRequest(r)
	if sess == nil || sess.Principal.Role != auth.RoleAdmin {
		response.Error(w, http.StatusForbidden, "FORBIDDEN", "管理员权限不足")
		return nil, "", false
	}
	tx, _, ok := h.beginCareRead(w, r, sess.Principal.ID, action)
	if !ok {
		return nil, "", false
	}
	return tx, fmt.Sprintf("admin:%d", sess.Principal.ID), true
}

func (h *Handler) riskAdminEvents(w http.ResponseWriter, r *http.Request) {
	tx, _, ok := h.riskAdmin(w, r, carepolicy.ReadRisk)
	if !ok {
		return
	}
	defer tx.Rollback()
	events, rules, err := h.Risk.HistoryMySQL(r.Context(), 100)
	if err != nil {
		response.Error(w, http.StatusServiceUnavailable, "AUDIT_UNAVAILABLE", "审计查询暂不可用")
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"events": events, "rules": rules})
}

func (h *Handler) riskAdminRuleCreate(w http.ResponseWriter, r *http.Request) {
	tx, actor, ok := h.riskAdmin(w, r, carepolicy.ManageRisk)
	if !ok {
		return
	}
	defer tx.Rollback()
	var input struct {
		IP            string `json:"ip"`
		Action        string `json:"action"`
		Reason        string `json:"reason"`
		DurationHours int    `json:"durationHours"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.Error(w, http.StatusBadRequest, "INVALID_RULE", "请求内容不合法")
		return
	}
	if input.DurationHours < 0 || input.DurationHours > 8760 {
		response.Error(w, http.StatusBadRequest, "INVALID_DURATION", "封禁期限不合法")
		return
	}
	rule, err := h.Risk.InsertRuleTx(r.Context(), tx, input.IP, input.Action, input.Reason, actor, time.Duration(input.DurationHours)*time.Hour)
	if err != nil {
		if errors.Is(err, risk.ErrInvalidRule) {
			response.Error(w, http.StatusBadRequest, "INVALID_RULE", "封禁规则不合法")
		} else {
			response.Error(w, http.StatusServiceUnavailable, "RISK_STORAGE_ERROR", "封禁规则保存失败")
		}
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	if err := h.Risk.ReloadRules(r.Context()); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "RISK_REFRESH_FAILED", "规则已提交，读取最新规则失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"rule": rule})
}

func (h *Handler) riskAdminRuleDelete(w http.ResponseWriter, r *http.Request) {
	tx, actor, ok := h.riskAdmin(w, r, carepolicy.ManageRisk)
	if !ok {
		return
	}
	defer tx.Rollback()
	deleted, err := h.Risk.RemoveRuleTx(r.Context(), tx, strings.TrimSpace(r.PathValue("id")), actor)
	if err != nil {
		response.Error(w, http.StatusServiceUnavailable, "RISK_STORAGE_ERROR", "封禁规则保存失败")
		return
	}
	if !deleted {
		response.Error(w, http.StatusNotFound, "RULE_NOT_FOUND", "规则不存在")
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	if err := h.Risk.ReloadRules(r.Context()); err != nil {
		response.Error(w, http.StatusServiceUnavailable, "RISK_REFRESH_FAILED", "规则已提交，读取最新规则失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"deleted": true})
}
