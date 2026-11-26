package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	dbstore "yihe-care/internal/db"
	"yihe-care/internal/response"
)

type noticeRecord struct {
	TextID   *int64  `json:"textId"`
	Title    *string `json:"title"`
	TextMain *string `json:"textMain"`
	Time     *string `json:"time"`
	PicID    *int64  `json:"picId"`
}

type noticeImage struct {
	PicID   *int64  `json:"picId"`
	PicName *string `json:"picName"`
}

const textColumns = `text_id, title, text_main, LEFT(DATE_FORMAT(time,'%Y-%m-%d %H:%i:%s.%f'),23) AS time, pic_id`

// POST /api/v1/notices/list —— 公告列表（标题模糊+分页）。
func (h *Handler) listNotices(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	page, pageSize, ok := strictPage(w, m)
	if !ok {
		return
	}
	title := str(m, "title")
	filter := " FROM " + socialTextTable + " WHERE (?='' OR title LIKE ?)"
	countSQL := "SELECT COUNT(*) AS total" + filter
	query := "SELECT " + textColumns + filter + " ORDER BY time DESC, text_id DESC"
	args := []any{title, "%" + title + "%"}
	var total int64
	limitSQL, fullArgs := pageArgs(args, page, pageSize)
	items := []noticeRecord{}
	err := dbstore.QueryEach(r.Context(), h.DB, func(rows *sql.Rows) error {
		var d noticeRecord
		if err := rows.Scan(&d.TextID, &d.Title, &d.TextMain, &d.Time, &d.PicID, &total); err != nil {
			return err
		}
		// The aggregate supplies one count row even when the page is empty.
		if d.TextID == nil {
			return nil
		}
		items = append(items, d)
		return nil
	}, "SELECT page.*, totals.total FROM ("+countSQL+
		") AS totals LEFT JOIN ("+query+limitSQL+") AS page ON TRUE ORDER BY page.time DESC, page.text_id DESC", append(args, fullArgs...)...)
	if internalError(w, err) {
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"texts": items, "total": total, "page": page, "pageSize": pageSize,
	})
}

// POST /api/v1/notices/detail —— 公告详情+关联图片。
func (h *Handler) getNotice(w http.ResponseWriter, r *http.Request) {
	m, ok := bodyMap(w, r)
	if !ok {
		return
	}
	textID, ok := numParam(m, "textId")
	if !ok {
		response.Fail(w, "参数不正确")
		return
	}
	var t noticeRecord
	err := h.DB.QueryRowContext(r.Context(), "SELECT "+textColumns+" FROM "+socialTextTable+" WHERE text_id=?", textID).Scan(
		&t.TextID, &t.Title, &t.TextMain, &t.Time, &t.PicID)
	if errors.Is(err, sql.ErrNoRows) {
		recordNotFound(w)
		return
	}
	if internalError(w, err) {
		return
	}
	pics := []noticeImage{}
	if t.PicID != nil {
		pics, ok = queryRows(h, w, r.Context(), func(rows *sql.Rows) (noticeImage, error) {
			var image noticeImage
			err := rows.Scan(&image.PicID, &image.PicName)
			return image, err
		}, "SELECT pic_id, pic_name FROM pic WHERE pic_id=?", *t.PicID)
		if !ok {
			return
		}
	}
	response.JSON(w, http.StatusOK, map[string]any{"text": t, "pics": pics})
}
