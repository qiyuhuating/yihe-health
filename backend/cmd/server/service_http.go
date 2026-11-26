package main

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"yihe-care/internal/apidoc"
	"yihe-care/internal/response"
)

func (s *databaseService) configErrorText() string {
	if s.configErr == nil {
		return ""
	}
	return "服务端配置有误，依赖数据库的功能不可用，自动重连不会恢复；请检查环境变量配置后重启服务"
}

func (s *databaseService) serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	if s.desktopToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Yihe-Desktop")), []byte(s.desktopToken)) != 1 {
		response.JSON(w, http.StatusUnauthorized, map[string]any{"message": "API 文档端点需要授权"})
		return
	}
	apidoc.ServeHTTP(w, r)
}

func (s *databaseService) serveServiceStatus(w http.ResponseWriter, r *http.Request, api *http.Handler) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "服务状态接口仅支持 GET")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.JSON(w, http.StatusOK, map[string]any{
		"databaseReady": api != nil,
		"primaryEngine": "mysql",
		"configError":   s.configErrorText(),
		"analytics":     map[string]bool{"configured": api != nil, "ready": api != nil},
	})
}

func isLocalBypassRoute(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		return true
	}
	return (r.URL.Path == "/api/v1/desktop/exit" || r.URL.Path == "/api/v1/auth/logout") && r.Method == http.MethodPost
}

func (s *databaseService) middleware(local http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" && r.Method == http.MethodGet {
			s.serveOpenAPISpec(w, r)
			return
		}
		if r.URL.Path == "/metrics" && r.Method == http.MethodGet {
			s.serveMetrics(w, r)
			return
		}
		api := s.api.Load()
		if r.URL.Path == "/api/v1/service/status" {
			s.serveServiceStatus(w, r, api)
			return
		}
		if isLocalBypassRoute(r) {
			local.ServeHTTP(w, r)
			return
		}
		if api == nil {
			w.Header().Set("Retry-After", "3")
			w.Header().Set("Cache-Control", "no-store")
			response.JSON(w, http.StatusServiceUnavailable, map[string]any{
				"code":    "DATABASE_UNAVAILABLE",
				"message": "数据库尚未连接，请稍后重试",
			})
			return
		}
		(*api).ServeHTTP(w, r)
	})
}

func boolNumber(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
