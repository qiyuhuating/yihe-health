// Package auth 复刻 SecurityConfig 的鉴权语义：
// session 登录 + ADMIN/FAMILY 角色 + 公开/硬件/SHARED 路径表。
package auth

import (
	"net/http"
	"strings"

	"yihe-care/internal/accesspolicy"
	"yihe-care/internal/observability"
	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

const (
	RoleAdmin  = "ADMIN"
	RoleFamily = "FAMILY"
)

// Middleware 鉴权：公开路径放行；其余按角色校验，会话注入 context。
func Middleware(store *session.Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 静态/健康检查放行
		if r.URL.Path == "/" || r.URL.Path == "/healthz" ||
			strings.HasPrefix(r.URL.Path, "/assets/") ||
			strings.HasPrefix(r.URL.Path, "/static/") ||
			r.URL.Path == "/index.html" || r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		access := accesspolicy.AccessFor(r.Method, r.URL.Path)
		if access == accesspolicy.Public || access == accesspolicy.Device {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			forbidden(w)
			return
		}

		sess := store.FromRequest(r)
		if sess == nil {
			response.JSON(w, http.StatusUnauthorized, map[string]any{
				"code":    "AUTH_REQUIRED",
				"message": "请重新登录",
			})
			return
		}

		role := sess.Principal.Role
		observability.SetAuthenticatedActor(r, sess.Principal.ID, role)
		if access == accesspolicy.Shared {
			if role != RoleAdmin && role != RoleFamily {
				forbidden(w)
				return
			}
		} else if role != RoleAdmin {
			forbidden(w)
			return
		}

		next.ServeHTTP(w, session.WithContext(r, sess))
	})
}

func forbidden(w http.ResponseWriter) {
	response.JSON(w, http.StatusForbidden, map[string]any{
		"code":    "ACCESS_DENIED",
		"message": "请求未获授权，请刷新页面后重试",
	})
}

// Principal 从 context 取当前登录用户，无则返回 nil。
func Principal(r *http.Request) *session.Principal {
	sess := session.FromContext(r)
	if sess == nil {
		return nil
	}
	p := sess.Principal
	return &p
}
