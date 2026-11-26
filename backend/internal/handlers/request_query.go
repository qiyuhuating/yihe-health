package handlers

import (
	"database/sql"
	"net/http"

	"yihe-care/internal/response"
)

// pagedTotal counts rows for a paginated query.
func (h *Handler) pagedTotal(w http.ResponseWriter, r *http.Request, sql string, args []any) (int64, bool) {
	var total int64
	if internalError(w, h.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM ("+sql+") AS paged_total", args...).Scan(&total)) {
		return 0, false
	}
	return total, true
}

// pageArgs 拼 LIMIT/OFFSET 占位符与完整参数。
func pageArgs(args []any, page, pageSize int) (string, []any) {
	full := make([]any, 0, len(args)+2)
	full = append(full, args...)
	full = append(full, pageSize, (page-1)*pageSize)
	return " LIMIT ? OFFSET ?", full
}

// rowsOK 检查 rows.Err()，有错写 500。
func rowsOK(w http.ResponseWriter, rows *sql.Rows) bool {
	if err := rows.Err(); err != nil {
		rows.Close()
		response.Error500(w, ref())
		return false
	}
	return true
}
