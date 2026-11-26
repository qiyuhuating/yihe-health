package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"yihe-care/internal/config"
	"time"

	"yihe-care/internal/care"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
)

// beginCareRead serializes sensitive reads with changes to the actor's
// explicit duty and administrator role. The caller must commit before replying.
func (h *Handler) beginCareRead(w http.ResponseWriter, r *http.Request, actor int64, action carepolicy.Action) (*sql.Tx, carepolicy.Duty, bool) {
	tx, duty, err := careworkflow.BeginAuthorized(r.Context(), h.DB, actor, action)
	if errors.Is(err, careworkflow.ErrCapabilityDenied) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return nil, "", false
	}
	if internalError(w, err) {
		return nil, "", false
	}
	return tx, duty, true
}

// A missing assignment has no privileges.
func (h *Handler) careDuty(w http.ResponseWriter, r *http.Request, actor int64) (string, bool) {
	duty, err := carerepo.Duty(r.Context(), h.DB, actor)
	if internalError(w, err) {
		return "", false
	}
	return duty, true
}

func (h *Handler) authorizeCareAction(w http.ResponseWriter, r *http.Request, actor int64, action string) bool {
	duty, ok := h.careDuty(w, r, actor)
	if !ok {
		return false
	}
	if !carepolicy.Allowed(carepolicy.Duty(duty), carepolicy.Action(action)) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return false
	}
	return true
}

// Log sensitive reads before returning personal or care history data. If the
// audit cannot be written, deny disclosure instead of silently skipping it.
func (h *Handler) auditCareAccess(w http.ResponseWriter, r *http.Request, actor int64, action string, incidentID *int64) bool {
	_, err := h.DB.ExecContext(r.Context(), "INSERT INTO care_access_audit(admin_id,action,incident_id) VALUES(?,?,?)", actor, action, incidentID)
	return !internalError(w, err)
}

// commitCareChange records the audit in the same transaction as the mutation.
func commitCareChange(ctx context.Context, tx *sql.Tx, actor int64, action string) error {
	if _, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action) VALUES(?,?)", actor, action); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *Handler) receiptConfigured() bool {
	secret := h.Cfg.Requests.CareReceiptSecret
	if !h.Cfg.Requests.Loaded {
		secret = config.Secret("CARE_RECEIPT_SECRET")
	}
	return len(secret) >= 32
}

func (h *Handler) careDiagnostics(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	tx, _, ok := h.beginCareRead(w, r, actor, carepolicy.Diagnostics)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'service_diagnostics')", actor); internalError(w, err) {
		return
	}
	diagnostics, err := careworkflow.ReadDiagnostics(r.Context(), tx, time.Now())
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{
		"pendingNotifications": diagnostics.PendingNotifications, "failedNotifications": diagnostics.FailedNotifications,
		"staleSending": diagnostics.StaleSending, "overdueIncidents": diagnostics.OverdueIncidents,
		"oldestOutstandingAt": diagnostics.OldestOutstandingAt,
		"workerHealthy":       diagnostics.WorkerHealthy, "workerLastSeenAt": diagnostics.WorkerLastSeenAt, "workerStartedAt": diagnostics.WorkerStartedAt,
		"primaryEngine": "mysql", "analytics": h.analyticsStatus(),
		"notificationConfigured": care.NotificationConfigured(), "receiptConfigured": h.receiptConfigured(),
		"note": "数据库统计不是网关送达或值班到岗证明",
	})
}

func (h *Handler) analyticsStatus() any {
	// The MySQL source is authoritative. Keep the diagnostics response shape
	// for clients that previously observed an optional projection worker.
	return map[string]any{"configured": h.DB != nil, "ready": h.DB != nil, "pending": 0, "lastSyncedAt": nil}
}
