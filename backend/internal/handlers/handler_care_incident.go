package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/carerepo"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/notify"
	"yihe-care/internal/response"
)

// Care incident endpoints are ADMIN-only: a family session cannot accept or close a care task.
func (h *Handler) routeCare(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/care/incidents", h.careIncidents)
	mux.HandleFunc("POST /api/v1/care/transition", h.careTransition)
	mux.HandleFunc("POST /api/v1/care/handover/create", h.careCreateHandover)
	mux.HandleFunc("POST /api/v1/care/handover/accept", h.careAcceptHandover)
	mux.HandleFunc("POST /api/v1/care/handover/cancel", h.careCancelHandover)
	mux.HandleFunc("POST /api/v1/care/events", h.careEvents)
	mux.HandleFunc("POST /api/v1/care/receipt", h.careReceipt)
	mux.HandleFunc("POST /api/v1/care/diagnostics", h.careDiagnostics)
	mux.HandleFunc("POST /api/v1/care/staff", h.careStaff)
	mux.HandleFunc("POST /api/v1/care/assignDuty", h.careAssignDuty)
	mux.HandleFunc("POST /api/v1/care/accounts/status", h.careAccountStatus)
	mux.HandleFunc("POST /api/v1/care/accessAudit", h.careAccessAudit)
}

func careAdmin(w http.ResponseWriter, r *http.Request) (int64, bool) {
	p, ok := principalOr401(w, r)
	if !ok {
		return 0, false
	}
	if p.Role != auth.RoleAdmin {
		response.JSON(w, http.StatusForbidden, map[string]any{"message": "无照护管理权限"})
		return 0, false
	}
	return p.ID, true
}

func (h *Handler) careIncidents(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := optBody(w, r)
	if !ok {
		return
	}
	page, pageSize, ok := strictPage(w, m)
	if !ok {
		return
	}
	state := str(m, "state")
	if state == "" {
		state = "active"
	}
	if state != "active" && state != "verified" {
		write400(w, "任务范围应为 active 或 verified")
		return
	}
	tx, duty, ok := h.beginCareRead(w, r, actor, carepolicy.ReadTasks)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'incident_list')", actor); internalError(w, err) {
		return
	}
	items, total, err := careworkflow.ListIncidents(r.Context(), tx, duty, actor, state == "verified", page, pageSize)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"items": items, "total": total, "page": page, "pageSize": pageSize, "duty": duty, "actorId": actor})
}

// Transitions are serialized with SELECT ... FOR UPDATE. A transition records its
// actor and note in the same transaction. Acknowledgment is distinct from closure.
func (h *Handler) careTransition(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, ok := requireID(w, m, "id")
	if !ok {
		return
	}
	action := str(m, "action")
	if !h.authorizeCareAction(w, r, actor, action) {
		return
	}
	note := strings.TrimSpace(str(m, "note"))
	if utf8.RuneCountInString(note) > 2000 {
		write400(w, "记录不可超过2000字符")
		return
	}
	ctx := r.Context()
	tx, err := h.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if internalError(w, err) {
		return
	}
	defer tx.Rollback()
	// The read-time role check is only for an early 403 response. Hold a
	// shared duty lock through commit so a concurrent demotion cannot race
	// this state transition.
	lockedDuty, err := carerepo.LockDuty(ctx, tx, actor)
	if internalError(w, err) {
		return
	}
	if !carepolicy.Allowed(carepolicy.Duty(lockedDuty), carepolicy.Action(action)) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
		return
	}
	before, resolver, acknowledgedBy, err := careworkflow.LockedIncidentState(ctx, tx, id)
	if err != nil {
		if isNotFound(err) {
			response.Fail(w, "事件不存在")
			return
		}
		response.Error500(w, ref())
		return
	}
	if !carepolicy.AllowedForIncident(carepolicy.Duty(lockedDuty), carepolicy.Action(action), actor, acknowledgedBy) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "只能处理自己接单的照护任务"})
		return
	}
	actorID := strconv.FormatInt(actor, 10)
	if action == "verify" && ((resolver != nil && *resolver == actorID) || (acknowledgedBy != nil && *acknowledgedBy == actorID)) {
		response.JSON(w, http.StatusForbidden, map[string]any{"message": "接单人、处置人不得复核本次照护任务"})
		return
	}
	next := careworkflow.NextState(before, action)
	if next == "" {
		response.JSON(w, http.StatusConflict, map[string]any{"message": "当前状态不允许此操作"})
		return
	}
	if (action == "resolve" || action == "verify" || action == "reopen") && note == "" {
		write400(w, "处置、复核和重新开启必须填写原因")
		return
	}
	note, err = h.sealNote(note)
	if internalError(w, err) {
		return
	}
	verdict := str(m, "verdict")
	if action == "verify" {
		// Empty verification verdict means confirmed; preserve the current API
		// contract while keeping review writes in the same incident transaction.
		if verdict == "" {
			verdict = "confirmed"
		}
		if !reviewVerdict(verdict) {
			write400(w, "复核结论须为 confirmed 或 false_alarm")
			return
		}
		if err := careworkflow.PersistReviewVerdict(ctx, tx, id, verdict, note, actor); err != nil {
			response.Error500(w, ref())
			return
		}
	}
	if internalError(w, careworkflow.PersistTransition(ctx, tx, id, next, action, actorID, note)) {
		return
	}
	if action == "escalate" && internalError(w, notify.QueueEscalationSMS(ctx, tx, h.Box, id, notify.EscalationPhone())) {
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"id": id, "state": next})
}

func (h *Handler) careEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, ok := requireID(w, m, "id")
	if !ok {
		return
	}
	page, pageSize, ok := strictPage(w, m)
	if !ok {
		return
	}
	tx, duty, ok := h.beginCareRead(w, r, actor, carepolicy.ReadTasks)
	if !ok {
		return
	}
	defer tx.Rollback()
	visible, err := careworkflow.CanReadIncident(r.Context(), tx, duty, actor, id)
	if internalError(w, err) {
		return
	}
	if !visible {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "任务不在可见范围"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO care_access_audit(admin_id,action,incident_id) VALUES(?,'incident_events',?)", actor, id); internalError(w, err) {
		return
	}
	events, total, err := careworkflow.ListEvents(r.Context(), tx, id, page, pageSize)
	if internalError(w, err) {
		return
	}
	for i := range events {
		if events[i].Note == nil {
			continue
		}
		plain, err := h.openNote(*events[i].Note)
		if internalError(w, err) {
			return
		}
		events[i].Note = &plain
	}
	if internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{
		"events": events, "total": total, "page": page, "pageSize": pageSize,
	})
}
