package socialworkflow

import (
	"context"
	"database/sql"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/socialrepo"
)

// ImageLocker is invoked only when an author actually has an avatar.
type ImageLocker func(context.Context, *sql.Tx, []string) error

// CreateComment keeps avatar ownership, image references and the comment
// insert in one transaction without exposing HTTP types to persistence.
func CreateComment(ctx context.Context, store *db.Database, box *securestore.Box, lockImages ImageLocker, comment socialrepo.NewComment) (bool, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	avatar, err := socialrepo.AuthorAvatar(ctx, tx, box, comment.AuthorPhone)
	if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if avatar != "" {
		if err := lockImages(ctx, tx, []string{avatar}); err != nil {
			return false, err
		}
	}
	comment.Avatar = avatar
	created, err := socialrepo.InsertComment(ctx, tx, box, comment)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return created, nil
}
