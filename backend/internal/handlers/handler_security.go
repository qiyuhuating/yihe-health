package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"yihe-care/internal/authrepo"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/observability"
	"yihe-care/internal/response"
	"yihe-care/internal/securityworkflow"
)

type factorGenerationKey struct{}

func (h *Handler) loginAudit(r *http.Request, phone, role string, id int64, outcome string) error {
	return h.loginAuditTx(r, phone, role, id, outcome, nil)
}
func (h *Handler) loginAuditTx(r *http.Request, phone, role string, id int64, outcome string, tx *sql.Tx) error {
	observability.Default.Add(`yihe_auth_attempts_total{outcome="`+outcome+`"}`, 1)
	requestID := r.Header.Get("X-Request-ID")
	if len(requestID) > 64 {
		requestID = requestID[:64]
	}
	return authrepo.RecordLoginAttempt(
		r.Context(), h.DB, tx, h.Box.Index("account", phone), h.Box.Index("ip", h.clientIP(r)),
		role, id, outcome, requestID,
	)
}

func (h *Handler) loginAllowed(w http.ResponseWriter, r *http.Request, phone string) bool {
	pair, account, err := authrepo.RecentFailureCounts(
		r.Context(), h.DB, h.Box.Index("account", phone),
		h.Box.Index("ip", h.clientIP(r)), time.Now().Add(-15*time.Minute),
	)
	if internalError(w, err) {
		return false
	}
	if pair >= 10 || account >= 30 {
		w.Header().Set("Retry-After", "900")
		response.JSON(w, 429, map[string]any{"message": "登录尝试过多，请稍后重试"})
		return false
	}
	return true
}
func (h *Handler) factorLogin(w http.ResponseWriter, r *http.Request, m map[string]any, role string, id int64, phone string) bool {
	valid, generation, err := securityworkflow.VerifyLogin(
		r.Context(), h.DB, h.Box, role, id, str(m, "otp"), str(m, "recoveryCode"),
		func(tx *sql.Tx, outcome string) error {
			return h.loginAuditTx(r, phone, role, id, outcome, tx)
		},
	)
	if errors.Is(err, authrepo.ErrFactorSecretUnavailable) {
		response.JSON(w, 503, map[string]any{"message": "安全密钥不可用"})
		return false
	}
	if internalError(w, err) {
		return false
	}
	if !valid {
		response.JSON(w, 401, map[string]any{"code": "MFA_REQUIRED", "message": "请输入有效的动态验证码或未使用的恢复码"})
		return false
	}
	if generation != "" {
		*r = *r.WithContext(context.WithValue(r.Context(), factorGenerationKey{}, generation))
	}
	return true
}
func (h *Handler) securityFactor(w http.ResponseWriter, r *http.Request) {
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	if h.Box == nil {
		response.JSON(w, 503, map[string]any{"message": "须先配置敏感字段加密密钥"})
		return
	}
	var req struct {
		Action   string `json:"action"`
		Password string `json:"password"`
		OTP      string `json:"otp"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	result, failure, err := securityworkflow.ChangeFactor(r.Context(), h.DB, h.Box, securityworkflow.FactorInput{
		Role: p.Role, ActorID: p.ID, Action: securityworkflow.FactorAction(req.Action),
		Password: req.Password, OTP: req.OTP, DatabaseSessions: h.Cfg.SessionBackend == "database",
	})
	if internalError(w, err) {
		return
	}
	switch failure {
	case securityworkflow.WrongPassword:
		response.JSON(w, 403, map[string]any{"message": "须验证当前密码"})
	case securityworkflow.AlreadyEnabled:
		conflict409(w, "已启用双因素认证，不能覆盖现有密钥")
	case securityworkflow.InvalidState:
		conflict409(w, "双因素认证状态不允许此操作")
	case securityworkflow.InvalidOTP:
		response.JSON(w, 403, map[string]any{"message": "动态验证码无效或已使用"})
	case securityworkflow.InvalidAction:
		write400(w, "操作须为 setup、confirm 或 disable")
	}
	if failure != "" {
		return
	}
	if req.Action != "setup" && h.Cfg.SessionBackend != "database" {
		if err := h.Sessions.RevokeContext(context.WithoutCancel(r.Context()), p.Role, p.ID); err != nil {
			response.JSON(w, 503, map[string]any{"message": "认证状态已更新，但会话撤销需重试"})
			return
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, result)
}

func (h *Handler) securityLoginAudit(w http.ResponseWriter, r *http.Request) {
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadLoginAudit)
	if !ok {
		return
	}
	defer tx.Rollback()
	var req struct {
		After int64 `json:"after"`
	}
	if !decodeCapability(w, r, &req) {
		return
	}
	if req.After < 0 {
		write400(w, "游标无效")
		return
	}
	items, err := authrepo.ListLoginAudits(r.Context(), tx, req.After)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"items": items})
}
