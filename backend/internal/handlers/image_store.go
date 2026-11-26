package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"yihe-care/internal/blob"

	"yihe-care/internal/response"
)

func (h *Handler) images() (*blob.Manager, error) {
	h.imageOnce.Do(func() {
		if h.DB == nil || h.Box == nil {
			h.imageError = errors.New("encrypted MySQL image storage requires a shared database and key")
			return
		}
		store, err := blob.NewMySQL(h.DB.DB)
		if err != nil {
			h.imageError = err
			return
		}
		h.imageManager = &blob.Manager{DB: h.DB, Store: &blob.Encrypted{Store: store, Box: h.Box}}
	})
	return h.imageManager, h.imageError
}

func (h *Handler) lockImages(w http.ResponseWriter, ctx context.Context, tx *sql.Tx, names []string) bool {
	if len(names) == 0 {
		return true
	}
	manager, err := h.images()
	if err == nil {
		err = manager.Require(ctx, tx, names)
	}
	if err == nil {
		return true
	}
	if errors.Is(err, blob.ErrNotFound) {
		write400(w, "图片不存在或已回收，请重新上传")
	} else {
		imageUnavailable(w)
	}
	return false
}

func imageUnavailable(w http.ResponseWriter) {
	response.JSON(w, http.StatusServiceUnavailable, map[string]any{"message": "图片存储暂不可用，请稍后重试"})
}
