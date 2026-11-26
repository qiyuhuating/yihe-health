package handlers

import (
	"database/sql"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"yihe-care/internal/auth"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

// principalOr401 requires the authenticated session principal.
func principalOr401(w http.ResponseWriter, r *http.Request) (*session.Principal, bool) {
	p := auth.Principal(r)
	if p == nil {
		response.JSON(w, http.StatusUnauthorized, map[string]any{"message": "请先登录"})
		return nil, false
	}
	return p, true
}

func (h *Handler) readScopedOldID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	body, ok := bodyMap(w, r)
	if !ok {
		return 0, false
	}
	return h.resolveOldID(w, r, body["oldId"])
}

func (h *Handler) resolveOptionalOldID(w http.ResponseWriter, r *http.Request, id int64) (int64, bool) {
	if id == 0 {
		return h.resolveOldID(w, r, nil)
	}
	return h.resolveOldID(w, r, id)
}

// beginScopedHealthRead retains health-data authorization until the caller
// commits its database read. It never inherits the staff-management capability.
func (h *Handler) beginScopedHealthRead(w http.ResponseWriter, r *http.Request, oldID int64) (*sql.Tx, bool) {
	return h.beginScopedResidentRead(w, r, oldID, carepolicy.ReadHealthData)
}

// beginScopedLocationRead uses the same family ownership lock, but administrators
// must hold the independent live location-reading capability.
func (h *Handler) beginScopedLocationRead(w http.ResponseWriter, r *http.Request, oldID int64) (*sql.Tx, bool) {
	return h.beginScopedResidentRead(w, r, oldID, carepolicy.ReadLocations)
}

// beginScopedResidentRead holds the actor's duty or family binding through
// the protected query. Role changes and family unbinding cannot overtake it.
func (h *Handler) beginScopedResidentRead(w http.ResponseWriter, r *http.Request, oldID int64, action carepolicy.Action) (*sql.Tx, bool) {
	return beginScopedResidentReadOn(w, r, h.DB.DB, oldID, action)
}

func beginScopedResidentReadOn(w http.ResponseWriter, r *http.Request, pool *sql.DB, oldID int64, action carepolicy.Action) (*sql.Tx, bool) {
	p, ok := principalOr401(w, r)
	if !ok {
		return nil, false
	}
	if p.Role == auth.RoleAdmin {
		tx, _, err := careworkflow.BeginAuthorized(r.Context(), pool, p.ID, action)
		if errors.Is(err, careworkflow.ErrCapabilityDenied) {
			response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED", "message": "岗位无此操作权限"})
			return nil, false
		}
		if internalError(w, err) {
			return nil, false
		}
		return tx, true
	}
	if p.Role != auth.RoleFamily {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
		return nil, false
	}
	tx, err := pool.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if internalError(w, err) {
		return nil, false
	}
	var id int64
	err = tx.QueryRowContext(r.Context(),
		"SELECT old_id FROM oldperson WHERE old_id=? AND family_id=? FOR SHARE", oldID, p.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		response.JSON(w, http.StatusForbidden, map[string]any{"message": "无权访问该账户或照护记录"})
		return nil, false
	}
	if internalError(w, err) {
		tx.Rollback()
		return nil, false
	}
	return tx, true
}

// resolveOldID allows ADMIN to select an elder by ID;
// FAMILY 传空用会话绑定的 oldId，传值则实时查库核对 oldperson.family_id。
func (h *Handler) resolveOldID(w http.ResponseWriter, r *http.Request, value any) (int64, bool) {
	p, ok := principalOr401(w, r)
	if !ok {
		return 0, false
	}
	id := p.OldID
	if value != nil {
		var valid bool
		id, valid = integerParam(value)
		if !valid {
			write400(w, "老人编号不正确")
			return 0, false
		}
	}
	if p.Role != auth.RoleAdmin {
		var familyID *int64
		err := h.DB.QueryRowContext(r.Context(), "SELECT family_id FROM oldperson WHERE old_id=?", id).Scan(&familyID)
		if err != nil || familyID == nil || *familyID != p.ID {
			response.JSON(w, http.StatusForbidden, map[string]any{"message": "无权访问该账户或照护记录"})
			return 0, false
		}
	}
	return id, true
}

func integerParam(value any) (int64, bool) {
	switch id := value.(type) {
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
		return parsed, err == nil
	case float64:
		// Converting an out-of-range float64 to int64 is implementation
		// dependent. Reject overflow and non-finite IDs before conversion.
		if math.IsNaN(id) || math.IsInf(id, 0) || id != math.Trunc(id) ||
			id < math.MinInt64 || id >= 9223372036854775808 {
			return 0, false
		}
		return int64(id), true
	case int:
		return int64(id), true
	case int64:
		return id, true
	default:
		return 0, false
	}
}
