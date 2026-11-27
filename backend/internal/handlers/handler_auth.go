// 会话、登录、初始化和桌面端控制接口。
package handlers

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/csrf"
	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

func (h *Handler) routeAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/auth/session", h.sessionInfo)
	mux.HandleFunc("POST /api/v1/auth/login", h.login)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
	mux.HandleFunc("POST /api/v1/setup", h.setup)
	mux.HandleFunc("POST /api/v1/desktop/exit", h.desktopExit)
}

// MountDesktopExit allows closing the desktop even before the database is ready.
func (h *Handler) MountDesktopExit(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/desktop/exit", h.desktopExit)
}

// MountLocalLogout keeps session cleanup available without a database.
func (h *Handler) MountLocalLogout(mux *http.ServeMux) {
	local := http.NewServeMux()
	local.HandleFunc("POST /api/v1/auth/logout", h.logout)
	mux.Handle("POST /api/v1/auth/logout", csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, local)))
}

// sessionInfo 处理 GET /api/v1/auth/session：公开；无会话则建匿名会话。
func (h *Handler) sessionInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	sess, err := h.Sessions.LookupRequest(r)
	if err != nil {
		response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "会话存储不可用"})
		return
	}
	if sess == nil {
		release, ok := admitAnonymousSession()
		if !ok {
			authenticationBusy(w, "yihe_auth_bootstrap_busy_total")
			return
		}
		defer release()
	}
	ctx := r.Context()
	var hasUsers bool
	if internalError(w, h.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users LIMIT 1)").Scan(&hasUsers)) {
		return
	}
	if sess == nil {
		sess = h.Sessions.CreateContext(ctx, session.Principal{})
		if sess == nil {
			response.JSON(w, 503, map[string]any{"message": "会话存储不可用"})
			return
		}
		session.SetCookie(w, sess.ID, h.CookieSecure)
	}
	authenticated := sess.Principal.Role == auth.RoleAdmin || sess.Principal.Role == auth.RoleFamily
	account := map[string]any{}
	if authenticated {
		p := sess.Principal
		account = map[string]any{
			"role": p.Role, "id": p.ID, "phone": p.Phone,
			"oldId": p.OldID, "displayName": p.DisplayName,
		}
		if p.Role == auth.RoleAdmin {
			duty, active, err := carerepo.AccountAccess(ctx, h.DB, p.ID)
			if internalError(w, err) {
				return
			}
			if !active {
				authenticated = false
				account = map[string]any{}
			} else {
				account["duty"] = duty
			}
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"authenticated": authenticated,
		"setupRequired": !hasUsers,
		"account":       account,
		"csrfToken":     sess.CSRFToken,
		"csrfHeader":    "X-CSRF-TOKEN",
	})
}

// login 处理 POST /api/v1/auth/login：公开；先查 users（管理员），再查 family（家属）。
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	phone := str(m, "phone")
	password := str(m, "password")
	if !auth.ValidPhone(phone) || password == "" || len(password) > 72 {
		response.WriteError(w, &response.AppError{HTTPStatus: http.StatusBadRequest, Code: "INVALID_LOGIN_INPUT", Message: "请输入有效手机号和密码"})
		return
	}

	if !accountLogins.acquire(phone) {
		authenticationBusy(w, "yihe_auth_account_busy_total")
		return
	}
	defer accountLogins.release(phone)
	if decision := h.Risk.BeforeLogin(h.clientIP(r), phone); !decision.Allowed() {
		h.Risk.RecordDenied(h.clientIP(r), phone, "login_restricted", r.UserAgent(),
			r.Header.Get("Sec-CH-UA-Model"), r.Header.Get("X-App-Device-ID"))
		h.riskDeniedLogin(w)
		return
	}
	if !h.loginAllowed(w, r, phone) {
		return
	}
	match, err := authrepo.Authenticate(r.Context(), h.DB, h.Box, phone, password)
	if internalError(w, err) {
		return
	}
	if match.Unlinked {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCOUNT_UNLINKED", "message": "该家属账号尚未关联老人，请联系管理员"})
		return
	}
	if match.Authenticated {
		h.finishAccountLogin(w, r, m, match.Principal, phone)
		return
	}
	if internalError(w, h.loginAudit(r, phone, "", 0, "invalid_credentials")) {
		return
	}
	h.Risk.FailedLogin(h.clientIP(r), phone, r.UserAgent(), r.Header.Get("Sec-CH-UA-Model"), r.Header.Get("X-App-Device-ID"))
	response.JSON(w, http.StatusUnauthorized, map[string]any{"code": "INVALID_CREDENTIALS", "message": "手机号或密码不正确"})
}

// renewSession 登录建会话：销毁请求 cookie 带来的旧会话（防 session fixation），建新会话并写 cookie。
func (h *Handler) renewSession(w http.ResponseWriter, r *http.Request, p session.Principal) bool {
	p.FactorGeneration, _ = r.Context().Value(factorGenerationKey{}).(string)
	if c, err := r.Cookie(session.CookieName); err == nil && c.Value != "" {
		if !h.Sessions.DestroyContext(r.Context(), c.Value) {
			response.JSON(w, 503, map[string]any{"message": "会话存储不可用"})
			return false
		}
	}
	sess := h.Sessions.CreateContext(r.Context(), p)
	if sess == nil {
		response.JSON(w, 503, map[string]any{"message": "会话存储不可用"})
		return false
	}
	if h.DB != nil && h.loginAudit(r, p.Phone, p.Role, p.ID, "success") != nil {
		// The new session has already been persisted. Compensate even if the
		// request was cancelled; the store still imposes its three-second bound.
		h.Sessions.DestroyContext(context.WithoutCancel(r.Context()), sess.ID)
		response.Error500(w, ref())
		return false
	}
	session.SetCookie(w, sess.ID, h.CookieSecure)
	return true
}

// logout 处理 POST /api/v1/auth/logout：需登录（中间件鉴权），销毁会话并清 cookie。
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	sess := session.FromContext(r)
	if sess == nil {
		sess = h.Sessions.FromRequest(r)
	}
	if sess != nil {
		if !h.Sessions.DestroyContext(r.Context(), sess.ID) {
			response.JSON(w, 503, map[string]any{"message": "会话撤销未完成"})
			return
		}
	}
	session.ClearCookie(w, h.CookieSecure)
	response.OK(w, nil)
}

// setup 处理 POST /api/v1/setup：公开 + 桌面 token；一次性初始化管理员账号。
func (h *Handler) setup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.checkDesktopToken(w, r) {
		return
	}
	release, ok := tryAdmission(loginAdmission())
	if !ok {
		authenticationBusy(w, "yihe_auth_setup_busy_total")
		return
	}
	defer release()
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	err := authrepo.InitializeAdmin(r.Context(), h.DB, h.Box, str(m, "name"), str(m, "phone"), str(m, "password"))
	switch {
	case errors.Is(err, authrepo.ErrAlreadyInitialized):
		response.JSON(w, http.StatusConflict, map[string]any{"message": "管理员已创建"})
	case errors.Is(err, authrepo.ErrInvalidAdmin):
		response.JSON(w, http.StatusBadRequest, map[string]any{"message": "请检查姓名、手机号和密码"})
	case internalError(w, err):
		return
	default:
		response.OK(w, nil)
	}
}

// desktopExit 处理 POST /api/v1/desktop/exit：公开 + 桌面 token；通过后延迟退出进程。
func (h *Handler) desktopExit(w http.ResponseWriter, r *http.Request) {
	if !h.checkDesktopToken(w, r) {
		return
	}
	response.WriteStruct(w, http.StatusOK, struct {
		Closing    bool   `json:"closing"`
		ReturnCode string `json:"returnCode"`
	}{Closing: true, ReturnCode: response.CodeOK})
	go func() {
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}()
}

// checkDesktopToken 校验 X-Yihe-Desktop header（常量时间比较）；失败写 403 空 body。
func (h *Handler) checkDesktopToken(w http.ResponseWriter, r *http.Request) bool {
	token := h.Cfg.DesktopToken
	got := r.Header.Get("X-Yihe-Desktop")
	if token == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		w.WriteHeader(http.StatusForbidden)
		return false
	}
	return true
}

func (h *Handler) finishAccountLogin(w http.ResponseWriter, r *http.Request, m map[string]any, p session.Principal, phone string) {
	if !h.factorLogin(w, r, m, p.Role, p.ID, phone) {
		return
	}
	if !h.renewSession(w, r, p) {
		return
	}
	h.Risk.SuccessfulLogin(h.clientIP(r), phone, r.UserAgent(), r.Header.Get("Sec-CH-UA-Model"), r.Header.Get("X-App-Device-ID"))
	response.WriteStruct(w, http.StatusOK, struct {
		Authenticated bool   `json:"authenticated"`
		ReturnCode    string `json:"returnCode"`
	}{Authenticated: true, ReturnCode: response.CodeOK})
}
