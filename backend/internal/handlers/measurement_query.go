package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"yihe-care/internal/carepolicy"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/response"
)

func listLatestMeasurements[T any](h *Handler, w http.ResponseWriter, r *http.Request, query, key string, scan func(*sql.Rows) (T, error)) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	page, pageSize, ok := strictPage(w, m)
	if !ok {
		return
	}
	name := str(m, "name")
	timeFirst := str(m, "timeFirst")
	timeLast := str(m, "timeLast")
	sql := "SELECT * FROM (" + query + ") t WHERE 1=1"
	var args []any
	if strings.TrimSpace(name) != "" {
		sql += " AND t.name LIKE ?"
		args = append(args, "%"+name+"%")
	}
	if strings.TrimSpace(timeFirst) != "" && strings.TrimSpace(timeLast) != "" {
		sql += " AND t.createTime BETWEEN ? AND ?"
		args = append(args, timeFirst, timeLast)
	}
	sql += " ORDER BY t.createTime DESC, t.oldId"
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadHealthData)
	if !ok {
		return
	}
	defer tx.Rollback()
	var total int64
	if internalError(w, tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM ("+sql+") AS paged_total", args...).Scan(&total)) {
		return
	}
	limitSQL, fullArgs := pageArgs(args, page, pageSize)
	items, err := dbstore.QueryList(r.Context(), tx, scan, sql+limitSQL, fullArgs...)
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		key: items, "total": total, "page": page, "pageSize": pageSize,
	})
}

func queryRows[T any](h *Handler, w http.ResponseWriter, ctx context.Context, scan func(*sql.Rows) (T, error), sql string, args ...any) ([]T, bool) {
	items, err := dbstore.QueryList(ctx, h.DB, scan, sql, args...)
	return items, !internalError(w, err)
}
