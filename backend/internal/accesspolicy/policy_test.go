package accesspolicy_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/accesspolicy"
	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

// Frozen preview@50b9cc0 access/CSRF tables, used to characterize this migration.
var publicPaths = map[string]bool{
	"POST /api/v1/desktop/exit":         true,
	"POST /api/v1/setup":                true,
	"POST /api/v1/auth/login":           true,
	"POST /api/v1/inviteCode/checkCode": true,
	"POST /api/v1/inviteCode/join":      true,
	"GET /api/v1/auth/session":          true,
}

// 硬件路径：公开且免 CSRF。
var hardwarePaths = map[string]bool{
	"POST /api/v1/device/commandPull": true,
	"POST /api/v1/device/commandAck":  true,
	"POST /api/v1/device/fencePoint":  true,
	"POST /api/v1/device/healthBatch": true,
	"POST /api/v1/device/login":       true,
	"POST /api/v1/device/bind":        true,
	"POST /api/v1/device/heartbeat":   true,
	"POST /api/v1/device/report":      true,
	"POST /api/v1/device/sos":         true,
	"POST /api/v1/care/receipt":       true,
}

// SHARED：ADMIN 或 FAMILY 任一角色。
var sharedPaths = map[string]bool{
	"POST /api/v1/security/factor":             true,
	"POST /api/v1/notifications/search":        true,
	"POST /api/v1/health/daily-query":          true,
	"POST /api/v1/health/changes":              true,
	"POST /api/v1/health/statistics":           true,
	"POST /api/v1/health/latest":               true,
	"POST /api/v1/auth/logout":                 true,
	"POST /api/v1/family/selectTwo":            true,
	"POST /api/v1/family/updateTwo":            true,
	"POST /api/v1/family/updateTouPic":         true,
	"POST /api/v1/family/updatePassword":       true,
	"POST /api/v1/vitals/history":              true,
	"POST /api/v1/vitals/chart":                true,
	"POST /api/v1/activity/history":            true,
	"POST /api/v1/activity/chart":              true,
	"POST /api/v1/position/selectFamily":       true,
	"POST /api/v1/notices/list":                true,
	"POST /api/v1/notices/detail":              true,
	"POST /api/v1/gallery/list":                true,
	"POST /api/v1/notice-comments/list":        true,
	"POST /api/v1/notice-comments/create":      true,
	"POST /api/v1/notice-comments/toggle-like": true,
	"POST /api/v1/image/saveImage":             true,
	"GET /api/v1/image/download":               true,
}

var exemptPaths = []string{
	"/api/v1/device/commandPull",
	"/api/v1/device/commandAck",
	"/api/v1/device/fencePoint",
	"/api/v1/setup",
	"/api/v1/device/login",
	"/api/v1/device/bind",
	"/api/v1/device/heartbeat",
	"/api/v1/device/report",
	"/api/v1/device/healthBatch",
	"/api/v1/device/sos",
	"/api/v1/care/receipt",
	"/api/v1/desktop/exit",
}

func oldAccess(method, path string) accesspolicy.Access {
	key := method + " " + path
	if publicPaths[key] {
		return accesspolicy.Public
	}
	if hardwarePaths[key] {
		return accesspolicy.Device
	}
	if sharedPaths[key] {
		return accesspolicy.Shared
	}
	return accesspolicy.Admin
}
func oldExempt(path string) bool {
	for _, p := range exemptPaths {
		if p == path {
			return true
		}
	}
	return false
}
func contractPaths() []string {
	paths := map[string]bool{"/api/v1/unknown": true, "/API/v1/device/sos": true, "/api/v1/device/sos/": true, "/bad": true}
	for _, table := range []map[string]bool{publicPaths, hardwarePaths, sharedPaths} {
		for key := range table {
			for i, c := range key {
				if c == ' ' {
					paths[key[i+1:]] = true
					break
				}
			}
		}
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	return result
}
func TestRoutePolicyCharacterization(t *testing.T) {
	for _, path := range contractPaths() {
		if got := accesspolicy.CSRFExempt(path); got != oldExempt(path) {
			t.Fatalf("CSRF exemption %s: %v", path, got)
		}
		for _, method := range []string{"GET", "POST", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			if got := accesspolicy.AccessFor(method, path); got != oldAccess(method, path) {
				t.Fatalf("access %s %s: %v", method, path, got)
			}
		}
	}
}
func TestMiddlewareRoleAndCSRFMatrix(t *testing.T) {
	for _, actor := range []struct {
		name, role string
		cookie     bool
	}{{"missing", "", false}, {"anonymous", "", true}, {"admin", auth.RoleAdmin, true}, {"family", auth.RoleFamily, true}, {"unknown", "OTHER", true}} {
		t.Run(actor.name, func(t *testing.T) {
			store := session.NewStore(time.Hour)
			t.Cleanup(store.Close)
			account := store.Create(session.Principal{Role: actor.role, ID: 1})
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
			gate := auth.Middleware(store, handler)
			csrfGate := csrf.Middleware(store, handler)
			for _, path := range contractPaths() {
				for _, method := range []string{"GET", "POST", "HEAD", "PUT", "PATCH", "DELETE", "OPTIONS"} {
					request := httptest.NewRequest(method, path, nil)
					if actor.cookie {
						request.AddCookie(&http.Cookie{Name: session.CookieName, Value: account.ID})
					}
					response := httptest.NewRecorder()
					gate.ServeHTTP(response, request)
					want := 204
					access := oldAccess(method, path)
					if path == "/bad" || path == "/API/v1/device/sos" {
						want = 403
					} else if access != accesspolicy.Public && access != accesspolicy.Device {
						if !actor.cookie {
							want = 401
						} else if actor.role != auth.RoleAdmin && (access != accesspolicy.Shared || actor.role != auth.RoleFamily) {
							want = 403
						}
					}
					if response.Code != want {
						t.Fatal(fmt.Sprintf("%s %s %s got=%d want=%d", actor.name, method, path, response.Code, want))
					}
					for _, token := range []bool{false, true} {
						check := request.Clone(request.Context())
						check.Header = request.Header.Clone()
						if token {
							check.Header.Set(csrf.HeaderName, account.CSRFToken)
						}
						got := httptest.NewRecorder()
						csrfGate.ServeHTTP(got, check)
						want := 204
						unsafe := method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
						if unsafe && !oldExempt(path) && (!actor.cookie || !token) {
							want = 403
						}
						if got.Code != want {
							t.Fatalf("CSRF %s %s %s token=%v got=%d want=%d", actor.name, method, path, token, got.Code, want)
						}
					}
				}
			}
		})
	}
}
