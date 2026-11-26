package handlers

import (
	"database/sql"
	"net/http"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/response"
)

// EnforceAdminDuty grants broad admin routes only to explicitly assigned supervisors.
// Run after authentication so the principal is available in request context.
func (h *Handler) EnforceAdminDuty(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal := auth.Principal(r)
		if principal == nil || principal.Role != auth.RoleAdmin {
			next.ServeHTTP(w, r)
			return
		}

		duty, active, err := carerepo.AccountAccess(r.Context(), h.DB, principal.ID)
		if err != nil {
			// An unavailable role store must not turn into an allow decision.
			response.JSON(w, http.StatusServiceUnavailable, map[string]any{
				"code":    "AUTHZ_UNAVAILABLE",
				"message": "岗位权限暂时无法验证",
			})
			return
		}
		if !active || (!restrictedAdminRouteAllowed(r) && duty != "supervisor") {
			// The authorization gate runs outside sensitiveCareAudit. Persist
			// denied access separately, without recording request content, query
			// arguments, names or unbounded paths. If the audit store is down,
			// remain fail-closed.
			if _, err := h.DB.ExecContext(r.Context(), `INSERT INTO care_sensitive_access_audit
				(actor_id,actor_role,resource,method) VALUES(?,?,?,?)`,
				principal.ID, principal.Role, "/api/v1/legacy-admin-denied", r.Method); err != nil {
				response.JSON(w, http.StatusServiceUnavailable, map[string]any{
					"code":    "AUTHZ_AUDIT_UNAVAILABLE",
					"message": "权限审计不可用，请稍后再试",
				})
				return
			}
			response.JSON(w, http.StatusForbidden, map[string]any{
				"code":    "ACCESS_DENIED",
				"message": "当前岗位无后台管理权限",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// beginAdminActionTx makes the requested capability part of the live-duty lock.
// The caller commits before responding or rolls back on failure.
func (h *Handler) beginAdminActionTx(w http.ResponseWriter, r *http.Request, action carepolicy.Action) (*sql.Tx, bool) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return nil, false
	}
	tx, _, ok := h.beginCareRead(w, r, actor, action)
	return tx, ok
}

// Legacy staff-management handlers use the same locked authorization boundary.
func (h *Handler) beginSupervisorTx(w http.ResponseWriter, r *http.Request) (*sql.Tx, bool) {
	return h.beginAdminActionTx(w, r, carepolicy.ManageStaff)
}

// Every care endpoint below performs its own fine-grained duty checks.
// MFA setup is self-service and must remain available to restricted staff.
// Do not permit arbitrary /api/v1/care/* here: new routes require review.
func restrictedAdminRouteAllowed(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/security/factor",
		"/api/v1/care/incidents",
		"/api/v1/care/transition",
		"/api/v1/care/events",
		"/api/v1/care/diagnostics",
		"/api/v1/care/staff",
		"/api/v1/care/assignDuty",
		"/api/v1/care/accessAudit":
		return true
	default:
		return false
	}
}
