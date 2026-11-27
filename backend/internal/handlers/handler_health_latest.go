package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/health"
	"yihe-care/internal/response"
)

// Latest is per source/device/metric/semantics, never a merged measurement.
func (h *Handler) healthLatest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldID *int64 `json:"oldId"`
	}
	if !decodeHealth(w, r, &req) {
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	oldID := p.OldID
	if req.OldID != nil {
		oldID = *req.OldID
	}
	if oldID < 1 {
		// Retain the error precedence for malformed resident IDs; valid IDs
		// are authorized once, inside the dedicated read transaction.
		if _, ok := h.resolveOldID(w, r, oldID); !ok {
			return
		}
		write400(w, "请指定有效的老人编号")
		return
	}
	tx, ok := beginScopedResidentReadOn(w, r, h.DB.StatisticsPool(), oldID, carepolicy.ReadHealthData, sql.LevelReadCommitted, nil)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, err := health.LatestInTransaction(r.Context(), tx, oldID, h.Box)
	if errors.Is(err, health.ErrLatestTooManySeries) {
		response.JSON(w, http.StatusRequestEntityTooLarge, map[string]any{"message": "最新采样候选超过 10000 条，请通过增量接口读取"})
		return
	}
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"oldId": oldID, "items": items})
}
