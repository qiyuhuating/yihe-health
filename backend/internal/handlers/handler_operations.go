package handlers

import (
	"net/http"
	"time"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/operationsrepo"
	"yihe-care/internal/response"
)

func (h *Handler) routeOperations(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/operations/snapshot", h.operationsSnapshot)
}

func (h *Handler) operationsSnapshot(w http.ResponseWriter, r *http.Request) {
	if _, ok := optBody(w, r); !ok {
		return
	}
	tx, ok := h.beginAdminActionTx(w, r, carepolicy.ReadOperations)
	if !ok {
		return
	}
	defer tx.Rollback()
	snapshot, err := operationsrepo.ReadSnapshot(r.Context(), tx, time.Now())
	if internalError(w, err) || internalError(w, tx.Commit()) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.OK(w, map[string]any{"snapshot": snapshot})
}
