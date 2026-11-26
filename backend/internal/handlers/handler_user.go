package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/db"
	"yihe-care/internal/guardianrepo"
	"yihe-care/internal/response"
	"yihe-care/internal/userphone"
)

// routeUser registers account and guardian routes.
func (h *Handler) routeUser(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/user/select", h.userSelect)
	mux.HandleFunc("POST /api/v1/guardians/list", h.listGuardians)
	mux.HandleFunc("POST /api/v1/guardians/create", h.createGuardian)
	mux.HandleFunc("POST /api/v1/guardians/update", h.updateGuardian)
	mux.HandleFunc("POST /api/v1/guardians/delete", h.deleteGuardian)
}

// POST /api/v1/user/select —— 按手机号查管理员账号（ADMIN）。
func (h *Handler) userSelect(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	phone := str(m, "userPhone")
	if phone == "" {
		response.Fail(w, "参数不完整")
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadAccounts)
	if !ok {
		return
	}
	defer tx.Rollback()
	lookup, err := userphone.SearchIndex(r.Context(), tx, h.Box, phone)
	if internalError(w, err) {
		return
	}
	list, err := db.QueryList(r.Context(), tx, func(rows *sql.Rows) (map[string]any, error) {
		var id int64
		var name, uphone, role *string
		var protected userphone.Record
		if err := rows.Scan(&id, &name, &uphone, &role, &protected.Cipher, &protected.Lookup, &protected.Protected); err != nil {
			return nil, err
		}
		protected.Legacy = uphone
		uphone, err := protected.OpenStored(h.Box, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"userId": id, "userName": name, "userPhone": uphone,
			"role": role, "password": nil,
		}, nil
	}, `SELECT user_id,user_name,user_phone,role,user_phone_cipher,user_phone_lookup,user_phone_protected FROM users
 WHERE (user_phone_protected=false AND user_phone=?) OR (user_phone_protected=true AND user_phone_lookup=?)`, phone, lookup)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"usersByPhone2": list})
}

// POST /api/v1/guardians/list —— 分页模糊查询客户（ADMIN）。
func (h *Handler) listGuardians(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	name := str(m, "customerName")
	page, pageSize, pageOK := bodyPage(w, m)
	if !pageOK {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadGuardians)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, total, err := guardianrepo.List(r.Context(), tx, h.Box, name, page, pageSize)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"customers": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

// POST /api/v1/guardians/create —— 新增客户（ADMIN）。
func (h *Handler) createGuardian(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	name, age, sex, phone, addr, ok := guardianFields(w, m)
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageGuardians)
	if !ok {
		return
	}
	defer tx.Rollback()
	affected, err := guardianrepo.Create(r.Context(), tx, h.Box, guardianrepo.Fields{
		Name: name, Age: age, Sex: sex, Phone: phone, Address: addr,
	})
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	writeSingleMutation(w, affected)
}

// POST /api/v1/guardians/update —— 更新客户（ADMIN）。
func (h *Handler) updateGuardian(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, okID := int64Val(m, "customerId")
	if !okID {
		response.Error500(w, ref())
		return
	}
	name, age, sex, phone, addr, ok := guardianFields(w, m)
	if !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageGuardians)
	if !ok {
		return
	}
	defer tx.Rollback()
	affected, err := guardianrepo.Update(r.Context(), tx, h.Box, id, guardianrepo.Fields{
		Name: name, Age: age, Sex: sex, Phone: phone, Address: addr,
	})
	if internalError(w, err) {
		return
	}
	if affected == 0 {
		// A missing row must not commit a newly reserved key-state marker.
		writeSingleMutation(w, 0)
		return
	}
	if internalError(w, tx.Commit()) {
		return
	}
	writeSingleMutation(w, affected)
}

// POST /api/v1/guardians/delete —— 删除客户（ADMIN）。
func (h *Handler) deleteGuardian(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	id, ok := int64Val(m, "customerId")
	if !ok {
		response.Error500(w, ref())
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ManageGuardians)
	if !ok {
		return
	}
	defer tx.Rollback()
	err := guardianrepo.Delete(r.Context(), tx, id)
	switch {
	case errors.Is(err, guardianrepo.ErrGuardianNotFound):
		response.Fail(w, "监护人不存在，请刷新后重新选择")
	case errors.Is(err, guardianrepo.ErrGuardianAssigned):
		response.Fail(w, "该监护人仍关联长者，请先调整长者档案")
	case internalError(w, err):
		return
	case internalError(w, tx.Commit()):
		return
	default:
		response.OK(w, nil)
	}
}

// Profile assignment and guardian deletion lock the same parent before changing references.
func lockCustomer(w http.ResponseWriter, r *http.Request, tx *sql.Tx, id int64) bool {
	var found int64
	if err := tx.QueryRowContext(r.Context(), "SELECT customer_id FROM customer WHERE customer_id=? FOR UPDATE", id).Scan(&found); err != nil {
		if isNotFound(err) {
			response.Fail(w, "监护人不存在，请刷新后重新选择")
		} else {
			response.Error500(w, ref())
		}
		return false
	}
	return true
}

func guardianFields(w http.ResponseWriter, m map[string]any) (string, int, string, string, string, bool) {
	const bad = ""
	name, okName := vstrRequired(w, m, "customerName", 50)
	if !okName {
		return bad, 0, "", "", "", false
	}
	ageStr, okAge := vstrRequired(w, m, "customerAge", 4)
	if !okAge {
		return bad, 0, "", "", "", false
	}
	sex, okSex := vstrRequired(w, m, "customerSex", 1)
	if !okSex {
		return bad, 0, "", "", "", false
	}
	phone, okPhone := vstrRequired(w, m, "customerPhone", 12)
	if !okPhone {
		return bad, 0, "", "", "", false
	}
	addr, okAddr := vstrRequired(w, m, "customerAddress", 200)
	if !okAddr {
		return bad, 0, "", "", "", false
	}
	age, err := strconv.Atoi(ageStr)
	if err != nil || age < 0 || age > 150 {
		write400(w, "年龄须为 0–150 岁的整数")
		return "", 0, "", "", "", false
	}
	return name, age, sex, phone, addr, true
}
