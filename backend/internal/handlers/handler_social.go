// Comments, announcements, gallery images and uploads.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"
	"yihe-care/internal/blob"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/response"
	"yihe-care/internal/socialrepo"
	"yihe-care/internal/socialworkflow"
)

func (h *Handler) routeSocial(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/notice-comments/list", h.listNoticeComments)
	mux.HandleFunc("POST /api/v1/notice-comments/search", h.searchNoticeComments)
	mux.HandleFunc("POST /api/v1/notice-comments/delete", h.deleteNoticeComment)
	mux.HandleFunc("POST /api/v1/notice-comments/create", h.createNoticeComment)
	mux.HandleFunc("POST /api/v1/notice-comments/reply", h.replyToNoticeComment)
	mux.HandleFunc("POST /api/v1/notice-comments/toggle-like", h.toggleNoticeCommentLike)
	mux.HandleFunc("POST /api/v1/notices/list", h.listNotices)
	mux.HandleFunc("POST /api/v1/notices/detail", h.getNotice)
	mux.HandleFunc("POST /api/v1/notices/create", h.createNotice)
	mux.HandleFunc("POST /api/v1/notices/delete", h.deleteNotice)
	mux.HandleFunc("POST /api/v1/notices/update", h.updateNotice)
	mux.HandleFunc("POST /api/v1/gallery/replace", h.replaceGallery)
	mux.HandleFunc("POST /api/v1/gallery/list", h.listGallery)
	mux.HandleFunc("POST /api/v1/image/saveImage", h.imageSave)
	mux.HandleFunc("GET /api/v1/image/download", h.imageDownload)
}

// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

// POST /api/v1/notice-comments/list —— 查询某公告下的说说/评论（含当前人点赞状态）。
func (h *Handler) listNoticeComments(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	textID, ok := numParam(m, "textId")
	if !ok {
		response.Fail(w, "参数不正确")
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	items, err := socialrepo.ListComments(r.Context(), h.DB, h.Box, p.Phone, textID)
	if internalError(w, err) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"comments": items})
}

// POST /api/v1/notice-comments/search —— 管理员分页查询全部说说。
func (h *Handler) searchNoticeComments(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	page, pageSize, ok := strictPage(w, m)
	if !ok {
		return
	}
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	items, total, err := socialworkflow.ModeratedComments(r.Context(), h.DB, actor, socialTextTable, page, pageSize)
	if errors.Is(err, careworkflow.ErrCapabilityDenied) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
		return
	}
	if internalError(w, err) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"moderatedComments": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

// POST /api/v1/notice-comments/delete —— 删除一条说说。
func (h *Handler) deleteNoticeComment(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	saidID, ok := numParam(m, "saidId")
	if !ok {
		response.Fail(w, "参数不正确")
		return
	}
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	deleted, err := socialworkflow.DeleteModeratedComment(r.Context(), h.DB, actor, saidID)
	if errors.Is(err, careworkflow.ErrCapabilityDenied) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
		return
	}
	if internalError(w, err) {
		return
	}
	writeCode(w, deleted)
}

// POST /api/v1/notice-comments/create —— 发表说说/评论。
func (h *Handler) createNoticeComment(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	v, exists := m["said"]
	if !exists || v == nil {
		response.Fail(w, "参数不正确")
		return
	}
	var saidContent string
	if s, isStr := v.(string); isStr {
		saidContent = s
	} else {
		saidContent = fmt.Sprintf("%v", v)
	}
	// 与前端 maxlength=30 对齐；仅约束新留言，历史数据不受影响。
	if utf8.RuneCountInString(saidContent) > 30 || saidContent == "" {
		response.Fail(w, "留言内容须为1-30个字符")
		return
	}
	textID, ok := numParam(m, "textId")
	if !ok {
		response.Fail(w, "参数不正确")
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	inserted, err := socialworkflow.CreateComment(r.Context(), h.DB, h.Box,
		func(ctx context.Context, tx *sql.Tx, names []string) error {
			manager, err := h.images()
			if err != nil {
				return err
			}
			return manager.Require(ctx, tx, names)
		}, socialrepo.NewComment{
			NoticeID: textID, Content: saidContent, CreatedAt: time.Now(),
			AuthorName: p.DisplayName, AuthorPhone: p.Phone,
		})
	if errors.Is(err, blob.ErrNotFound) {
		write400(w, "图片不存在或已回收，请重新上传")
		return
	}
	if internalError(w, err) {
		return
	}
	writeCode(w, inserted)
}

// POST /api/v1/notice-comments/reply —— 管理员回复说说。
func (h *Handler) replyToNoticeComment(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	saidID, ok := numParam(m, "saidId")
	if !ok {
		response.Fail(w, "参数不正确")
		return
	}
	v, exists := m["back"]
	if !exists || v == nil {
		response.Fail(w, "参数不正确")
		return
	}
	back, _ := v.(string)
	if utf8.RuneCountInString(back) > 2000 {
		response.Fail(w, "回复内容最多 2000 个字符")
		return
	}
	actor, ok := careAdmin(w, r)
	if !ok {
		return
	}
	updated, err := socialworkflow.ReplyModeratedComment(r.Context(), h.DB, actor, saidID, back)
	if errors.Is(err, careworkflow.ErrCapabilityDenied) {
		response.JSON(w, http.StatusForbidden, map[string]any{"code": "ACCESS_DENIED"})
		return
	}
	if internalError(w, err) {
		return
	}
	writeCode(w, updated)
}

// POST /api/v1/notice-comments/toggle-like —— 点赞/取消点赞（事务内执行，行锁防并发）。
func (h *Handler) toggleNoticeCommentLike(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	saidID, ok := numParam(m, "saidId")
	if !ok {
		response.Fail(w, "参数不正确")
		return
	}
	p, ok := principalOr401(w, r)
	if !ok {
		return
	}
	changed, err := socialrepo.ToggleLike(r.Context(), h.DB, h.Box, saidID, p.Phone)
	if isNotFound(err) {
		response.JSON(w, http.StatusNotFound, map[string]any{"message": "评论不存在"})
		return
	}
	if internalError(w, err) {
		return
	}
	writeCode(w, changed)
}

// ---------------------------------------------------------------------------
