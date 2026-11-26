package main

import (
	"net/http"
	"strings"
	dbstore "yihe-care/internal/db"

	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

// Audit authenticated requests to endpoints that expose personal, health,
// location or device information. No request bodies, phone numbers, location
// coordinates, bearer tokens or query strings are stored.
func sensitiveCareAudit(pool *dbstore.Database, next http.Handler) http.Handler {
	prefixes := []string{
		"/api/v1/residents/", "/api/v1/family/", "/api/v1/position/", "/api/v1/locations/alerts/",
		"/api/v1/vitals/", "/api/v1/activity/", "/api/v1/device/",
		"/api/v1/monitoring-profiles/", "/api/v1/image/download",
		"/api/v1/fence/", "/api/v1/fence-events/", "/api/v1/alert/", "/api/v1/alert-reviews/", "/api/v1/health/daily-query", "/api/v1/health/alerts/",
		"/api/v1/security/",
		"/api/v1/notification/",
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		audit := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				audit = true
				break
			}
		}
		// Public device telemetry endpoints do not carry a user session;
		// recording an anonymous actor here would be misleading.
		sess := session.FromContext(r)
		if !audit || sess == nil || sess.Principal.Role == "" {
			next.ServeHTTP(w, r)
			return
		}
		_, err := pool.ExecContext(r.Context(),
			"INSERT INTO care_sensitive_access_audit(actor_id,actor_role,resource,method) VALUES(?,?,?,?)",
			sess.Principal.ID, sess.Principal.Role, r.URL.Path, r.Method)
		if err != nil {
			response.JSON(w, http.StatusServiceUnavailable, map[string]any{
				"message": "敏感操作审计不可用，请稍后重试",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}
