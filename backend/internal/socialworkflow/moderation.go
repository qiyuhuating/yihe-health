package socialworkflow

import (
	"context"
	"time"

	"yihe-care/internal/carepolicy"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db"
	"yihe-care/internal/socialrepo"
)

func ModeratedComments(ctx context.Context, store *db.Database, actor int64, table string, page, pageSize int) ([]socialrepo.ModeratedComment, int64, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ReadModeration)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	items, total, err := socialrepo.SearchModeratedComments(ctx, tx, table, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func DeleteModeratedComment(ctx context.Context, store *db.Database, actor, id int64) (bool, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ManageContent)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	deleted, err := socialrepo.DeleteComment(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if deleted {
		if err := auditContentChange(ctx, tx, actor, "notice_comment_deleted"); err != nil {
			return false, err
		}
	}
	return deleted, tx.Commit()
}

func ReplyModeratedComment(ctx context.Context, store *db.Database, actor, id int64, reply string) (bool, error) {
	tx, _, err := careworkflow.BeginAuthorized(ctx, store, actor, carepolicy.ManageContent)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	updated, err := socialrepo.ReplyComment(ctx, tx, id, reply, time.Now())
	if err != nil {
		return false, err
	}
	if updated {
		if err := auditContentChange(ctx, tx, actor, "notice_comment_replied"); err != nil {
			return false, err
		}
	}
	return updated, tx.Commit()
}
