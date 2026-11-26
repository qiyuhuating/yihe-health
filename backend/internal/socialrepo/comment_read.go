package socialrepo

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

// Comment is the public notice-comment read model.
type Comment struct {
	SaidID     *int64  `json:"saidId"`
	Said       *string `json:"said"`
	TextID     *int64  `json:"textId"`
	Back       *string `json:"back"`
	SaidTime   *string `json:"saidTime"`
	BackTime   *string `json:"backTime"`
	PersonName *string `json:"personName"`
	NiceCount  *int64  `json:"niceCount"`
	SaidPhone  *string `json:"saidPhone"`
	TouPic     *string `json:"touPic"`
	Good       *string `json:"good"`
}

func maskPhone(phone *string) *string {
	if phone == nil || len(*phone) != 11 {
		return nil
	}
	value := *phone
	masked := value[:3] + "****" + value[7:]
	return &masked
}

// ListComments returns a notice's comments and the viewer's own vote status.
// A zero noticeID selects all comments using the existing endpoint semantics.
func ListComments(ctx context.Context, store *db.Database, box *securestore.Box, viewerPhone string, noticeID int64) ([]Comment, error) {
	lookup, err := socialPhoneIndex(ctx, store, box, viewerPhone)
	if err != nil {
		return nil, err
	}
	query := `SELECT s.said_id, s.said, s.text_id, s.back,` +
		` LEFT(DATE_FORMAT(s.said_time,'%Y-%m-%d %H:%i:%s.%f'),23) AS saidTime,` +
		` LEFT(DATE_FORMAT(s.back_time,'%Y-%m-%d %H:%i:%s.%f'),23) AS backTime,` +
		` s.person_name, s.nice_count, s.said_phone, s.said_phone_cipher, s.said_phone_protected, s.tou_pic,` +
		` CASE WHEN EXISTS (SELECT 1 FROM nice_count n WHERE n.said_id=s.said_id AND ` + likePhoneMatch + `) THEN '1' ELSE '0' END AS good` +
		` FROM said s`
	args := []any{viewerPhone, lookup}
	if noticeID != 0 {
		query += " WHERE s.text_id=?"
		args = append(args, noticeID)
	}
	return db.QueryList(ctx, store, func(rows *sql.Rows) (Comment, error) {
		var item Comment
		var cipher *string
		var protected bool
		err := rows.Scan(&item.SaidID, &item.Said, &item.TextID, &item.Back, &item.SaidTime, &item.BackTime,
			&item.PersonName, &item.NiceCount, &item.SaidPhone, &cipher, &protected, &item.TouPic, &item.Good)
		if err == nil {
			if item.SaidID == nil {
				return item, errors.New("missing comment identity")
			}
			item.SaidPhone, err = openCommentPhone(box, *item.SaidID, item.SaidPhone, cipher, protected)
			if err == nil {
				item.SaidPhone = maskPhone(item.SaidPhone)
			}
		}
		return item, err
	}, query, args...)
}

type ModeratedComment struct {
	SaidID     *int64  `json:"saidId"`
	Said       *string `json:"said"`
	Back       *string `json:"back"`
	SaidTime   *string `json:"saidTime"`
	BackTime   *string `json:"backTime"`
	PersonName *string `json:"personName"`
	TouPic     *string `json:"touPic"`
	Title      *string `json:"title"`
}

// SearchModeratedComments returns one page and the full count of comments.
func SearchModeratedComments(ctx context.Context, store db.ReadQuerier, noticeTable string, page, pageSize int) ([]ModeratedComment, int64, error) {
	query := `SELECT said_id, said, back,` +
		` LEFT(DATE_FORMAT(said_time,'%Y-%m-%d %H:%i:%s.%f'),23) AS saidTime,` +
		` LEFT(DATE_FORMAT(back_time,'%Y-%m-%d %H:%i:%s.%f'),23) AS backTime,` +
		` person_name, tou_pic, title FROM said INNER JOIN ` + noticeTable +
		` ON said.text_id=` + noticeTable + `.text_id` +
		` ORDER BY said_time DESC, said_id DESC`
	var total int64
	if err := store.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+query+") AS paged_total").Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := db.QueryList(ctx, store, func(rows *sql.Rows) (ModeratedComment, error) {
		var item ModeratedComment
		err := rows.Scan(&item.SaidID, &item.Said, &item.Back, &item.SaidTime, &item.BackTime, &item.PersonName, &item.TouPic, &item.Title)
		return item, err
	}, query+" LIMIT ? OFFSET ?", pageSize, (page-1)*pageSize)
	return items, total, err
}
