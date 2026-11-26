package handlers

import (
	"errors"
	"net/http"
	"strings"

	"yihe-care/internal/blob"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
	"yihe-care/internal/socialrepo"
	"yihe-care/internal/socialworkflow"
)

// POST /api/v1/gallery/replace —— 替换首页展示图（整体替换，事务内执行）。
func (h *Handler) replaceGallery(w http.ResponseWriter, r *http.Request) {
	m, ok := optBody(w, r)
	if !ok {
		return
	}
	v, exists := m["fileListName"]
	arr, isList := v.([]any)
	if !exists || v == nil || !isList || len(arr) == 0 || len(arr) > 10 {
		writeCode(w, false)
		return
	}
	names := make([]string, 0, len(arr))
	for _, e := range arr {
		s, isStr := e.(string)
		if !isStr || strings.TrimSpace(s) == "" || strings.Contains(s, "/") || strings.Contains(s, "\\") {
			writeCode(w, false)
			return
		}
		names = append(names, s)
	}
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	manager, err := h.images()
	if internalError(w, err) {
		return
	}
	if err := socialworkflow.ReplaceGallery(r.Context(), h.DB, actor, manager, names); err != nil {
		switch {
		case errors.Is(err, careworkflow.ErrCapabilityDenied):
			response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
		case errors.Is(err, blob.ErrNotFound):
			write400(w, "图片不存在或已回收，请重新上传")
		default:
			internalError(w, err)
		}
		return
	}
	writeCode(w, true)
}

// POST /api/v1/gallery/list —— 查询展示图列表。
func (h *Handler) listGallery(w http.ResponseWriter, r *http.Request) {
	if _, ok := optBody(w, r); !ok {
		return
	}
	items, err := socialrepo.ListGallery(r.Context(), h.DB)
	if internalError(w, err) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"galleryImages": items})
}

// ---------------------------------------------------------------------------
