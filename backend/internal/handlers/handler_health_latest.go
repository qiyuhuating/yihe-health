package handlers

import (
	"errors"
	"net/http"

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
	var requested any
	if req.OldID != nil {
		requested = *req.OldID
	}
	oldID, ok := h.resolveOldID(w, r, requested)
	if !ok {
		return
	}
	if oldID < 1 {
		write400(w, "请指定有效的老人编号")
		return
	}
	tx, ok := h.beginScopedHealthRead(w, r, oldID)
	if !ok {
		return
	}
	defer tx.Rollback()
	items, err := health.Latest(r.Context(), h.DB, oldID, h.Box)
	if errors.Is(err, health.ErrLatestTooManySeries) {
		response.JSON(w, http.StatusRequestEntityTooLarge, map[string]any{"message": "最新采样候选超过 10000 条，请通过增量接口读取"})
		return
	}
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	response.OK(w, map[string]any{"oldId": oldID, "items": items})
}
