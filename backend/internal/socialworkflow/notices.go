package socialworkflow

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/blob"
	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
)

var ErrNoticeNotFound = errors.New("notice not found")

const noticeTable = "`text`"

func noticePictureGroup(ctx context.Context, tx *sql.Tx, id int64) (*int64, error) {
	var group *int64
	err := tx.QueryRowContext(ctx, "SELECT pic_id FROM "+noticeTable+" WHERE text_id=? FOR UPDATE", id).Scan(&group)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoticeNotFound
	}
	return group, err
}

func allocateNoticePictureGroup(ctx context.Context, tx *sql.Tx) (int64, error) {
	return db.InsertID(ctx, tx, "INSERT INTO picture_group () VALUES ()")
}

func replaceNoticePictures(ctx context.Context, tx *sql.Tx, group int64, names []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM pic WHERE pic_id=?", group); err != nil {
		return err
	}
	for _, name := range names {
		result, err := tx.ExecContext(ctx, "INSERT INTO pic(pic_id,pic_name) VALUES(?,?)", group, name)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return errors.New("notice image insert affected unexpected number of rows")
		}
	}
	return nil
}

// UpdateNotice locks the notice before reading its image group, avoiding a
// separate pre-transaction read and concurrent image-group replacement.
func UpdateNotice(ctx context.Context, store *db.Database, actor int64, manager *blob.Manager, id int64, title, body string, names []string) (bool, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ManageContent)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	group, err := noticePictureGroup(ctx, tx, id)
	if errors.Is(err, ErrNoticeNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(names) > 0 {
		if err := manager.Require(ctx, tx, names); err != nil {
			return false, err
		}
	}
	var groupID int64
	if group == nil {
		groupID, err = allocateNoticePictureGroup(ctx, tx)
		if err != nil {
			return false, err
		}
	} else {
		groupID = *group
	}
	if err := replaceNoticePictures(ctx, tx, groupID, names); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, "UPDATE "+noticeTable+" SET title=?,text_main=?,time=?,pic_id=? WHERE text_id=?", title, body, time.Now(), groupID, id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, nil
	}
	if err := auditContentChange(ctx, tx, actor, "notice_updated"); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func DeleteNotice(ctx context.Context, store *db.Database, actor, id int64) (bool, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ManageContent)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	group, err := noticePictureGroup(ctx, tx, id)
	if errors.Is(err, ErrNoticeNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM "+noticeTable+" WHERE text_id=?", id)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, nil
	}
	if group != nil {
		if _, err := tx.ExecContext(ctx, "DELETE FROM pic WHERE pic_id=?", *group); err != nil {
			return false, err
		}
	}
	if err := auditContentChange(ctx, tx, actor, "notice_deleted"); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func auditContentChange(ctx context.Context, tx *sql.Tx, actor int64, action string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO care_access_audit(admin_id,action) VALUES(?,?)", actor, action)
	return err
}
