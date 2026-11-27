// Package csrf 实现基于会话的 CSRF token 校验：
//   - GET /api/v1/auth/session 建立会话并下发 token；
//   - 所有 POST/PUT/DELETE/PATCH（除豁免路径）必须带 X-CSRF-TOKEN 头且与会话 token 一致，
//     无会话或 token 不对 → 403 CSRF_REJECTED；
//   - 硬件接口使用设备鉴权，setup 与 desktop/exit 使用桌面令牌，不依赖浏览器 CSRF。
//
// 挂载顺序：限流 → csrf → auth → 路由。
package csrf

import (
	"crypto/subtle"
	"net/http"

	"yihe-care/internal/accesspolicy"
	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

const HeaderName = "X-CSRF-TOKEN"

func rejected(w http.ResponseWriter) {
	response.Error(w, http.StatusForbidden, "CSRF_REJECTED", "请求未获授权，请刷新页面后重试")
}

// Middleware 校验 CSRF token。
func Middleware(store *session.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accesspolicy.CSRFExempt(r.URL.Path) ||
			((r.Method == http.MethodGet || r.Method == http.MethodHead) && accesspolicy.PublicAssetPath(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			sess := store.FromRequest(r)
			if sess == nil {
				rejected(w)
				return
			}
			got := r.Header.Get(HeaderName)
			if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRFToken)) != 1 {
				rejected(w)
				return
			}
			next.ServeHTTP(w, session.WithContext(r, sess))
		default:
			if sess := store.FromRequest(r); sess != nil {
				r = session.WithContext(r, sess)
			}
			next.ServeHTTP(w, r)
		}
	})
}
