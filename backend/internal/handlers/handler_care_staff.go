package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/response"
)

func (h *Handler) careStaff(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	tx, _, ok := h.beginCareRead(w, r, actor, carepolicy.ManageStaff)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'staff_list')", actor); internalError(w, err) {
		return
	}
	list, err := db.QueryList(r.Context(), tx, func(rows *sql.Rows) (map[string]any, error) {
		var id int64
		var name *string
		var duty string
		if err := rows.Scan(&id, &name, &duty); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "name": name, "duty": duty}, nil
	}, `SELECT u.user_id,u.user_name,
 COALESCE(p.duty,'unassigned') FROM users u
 LEFT JOIN care_staff_permission p ON p.admin_id=u.user_id
 WHERE u.role='1'
 ORDER BY u.user_id LIMIT 100`)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"staff": list})
}

func (h *Handler) careAssignDuty(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	targetString, valid := vstr(m, "adminId")
	if !valid || !validID(targetString) {
		writeValidation400(w)
		return
	}
	target, err := strconv.ParseInt(targetString, 10, 64)
	if err != nil || target < 1 {
		writeValidation400(w)
		return
	}
	duty := str(m, "duty")
	if duty != "supervisor" && duty != "caregiver" && duty != "auditor" && duty != "unassigned" {
		write400(w, "无效岗位")
		return
	}
	err = careworkflow.AssignDuty(r.Context(), h.DB, actor, target, duty)
	switch {
	case errors.Is(err, careworkflow.ErrStaffMissing):
		response.Fail(w, "管理员账号不存在")
		return
	case errors.Is(err, careworkflow.ErrSelfDemotion):
		write400(w, "主管不能降低自己的岗位权限")
		return
	case errors.Is(err, careworkflow.ErrDutyDenied):
		response.JSON(w, http.StatusForbidden, map[string]any{"message": "岗位无此操作权限"})
		return
	case internalError(w, err):
		return
	}
	response.OK(w, map[string]any{"adminId": target, "duty": duty})
}

func (h *Handler) careAccessAudit(w http.ResponseWriter, r *http.Request) {
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	tx, _, ok := h.beginCareRead(w, r, actor, carepolicy.ReadAudit)
	if !ok {
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO care_access_audit(admin_id,action) VALUES(?,'audit_view')", actor); internalError(w, err) {
		return
	}
	items, err := db.QueryList(r.Context(), tx, func(rows *sql.Rows) (map[string]any, error) {
		var id int64
		var role, resource, method string
		var created interface{}
		if err := rows.Scan(&id, &role, &resource, &method, &created); err != nil {
			return nil, err
		}
		return map[string]any{"actorId": id, "role": role, "resource": resource, "method": method, "at": created}, nil
	}, `SELECT actor_id,actor_role,resource,method,created_at
 FROM care_sensitive_access_audit ORDER BY id DESC LIMIT 100`)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"accesses": items})
}
