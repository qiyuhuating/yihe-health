// Package socialrepo owns persisted notice-comment operations.
package socialrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

// DeleteComment removes one notice comment and reports whether a row was removed.
func DeleteComment(ctx context.Context, tx *sql.Tx, commentID int64) (bool, error) {
	var locked int64
	err := tx.QueryRowContext(ctx, "SELECT said_id FROM said WHERE said_id=? FOR UPDATE", commentID).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM nice_count WHERE said_id=?", commentID); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM said WHERE said_id=?", commentID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// ReplyComment updates the moderator reply and its timestamp.
func ReplyComment(ctx context.Context, tx *sql.Tx, commentID int64, reply string, at time.Time) (bool, error) {
	result, err := tx.ExecContext(ctx,
		"UPDATE said SET back=?, back_time=? WHERE said_id=?",
		reply, at, commentID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// ToggleLike serializes all votes for a comment under its row lock.
// Any failure rolls back both the vote and the aggregate count.
func ToggleLike(ctx context.Context, store *db.Database, box *securestore.Box, commentID int64, phone string) (bool, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	lookup, err := bindSocialPhoneKey(ctx, tx, box, phone)
	if err != nil {
		return false, err
	}

	var locked int64
	if err := tx.QueryRowContext(ctx, "SELECT said_id FROM said WHERE said_id=? FOR UPDATE", commentID).Scan(&locked); err != nil {
		return false, err
	}
	var count int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(1) FROM nice_count WHERE said_id=? AND "+likePhoneMatch, commentID, phone, lookup).Scan(&count); err != nil {
		return false, err
	}
	if count > 1 {
		return false, errors.New("duplicate historical votes require review")
	}
	var affected int64
	if count > 0 {
		result, err := tx.ExecContext(ctx, "DELETE FROM nice_count WHERE said_id=? AND "+likePhoneMatch, commentID, phone, lookup)
		if err != nil {
			return false, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		affected = changed
		if _, err := tx.ExecContext(ctx, "UPDATE said SET nice_count=nice_count-1 WHERE said_id=?", commentID); err != nil {
			return false, err
		}
	} else {
		var legacy, index any = phone, nil
		if box != nil {
			legacy, index = nil, lookup
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO nice_count(said_id,phone,phone_lookup,phone_protected) VALUES(?,?,?,?)", commentID, legacy, index, box != nil)
		if err != nil {
			return false, err
		}
		updated, err := result.RowsAffected()
		if err != nil {
			return false, err
		}
		affected = updated
		if _, err := tx.ExecContext(ctx, "UPDATE said SET nice_count=nice_count+1 WHERE said_id=?", commentID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return affected == 1, nil
}

type NewComment struct {
	NoticeID    int64
	Content     string
	AuthorName  string
	AuthorPhone string
	Avatar      string
	CreatedAt   time.Time
}

// AuthorAvatar retrieves the current family profile image for a comment author.
func AuthorAvatar(ctx context.Context, tx *sql.Tx, box *securestore.Box, phone string) (string, error) {
	var avatar *string
	lookup, err := userphone.SearchIndex(ctx, tx, box, phone)
	if err != nil {
		return "", err
	}
	err = tx.QueryRowContext(ctx, "SELECT family_tou_pic FROM family WHERE "+userphone.FamilyMatch, phone, lookup).Scan(&avatar)
	if err != nil {
		return "", err
	}
	if avatar == nil {
		return "", nil
	}
	return *avatar, nil
}

// InsertComment writes a notice comment inside the caller's transaction.
func InsertComment(ctx context.Context, tx *sql.Tx, box *securestore.Box, comment NewComment) (bool, error) {
	if _, err := bindSocialPhoneKey(ctx, tx, box, comment.AuthorPhone); err != nil {
		return false, err
	}
	var legacy any = comment.AuthorPhone
	if box != nil {
		legacy = nil
	}
	result, err := tx.ExecContext(ctx,
		"INSERT INTO said(said,text_id,said_time,person_name,said_phone,tou_pic) VALUES(?,?,?,?,?,?)",
		comment.Content, comment.NoticeID, comment.CreatedAt, comment.AuthorName, legacy, comment.Avatar)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected != 1 {
		return false, errors.New("comment insert affected an unexpected number of rows")
	}
	if box != nil {
		id, err := result.LastInsertId()
		if err != nil {
			return false, err
		}
		cipher, err := sealCommentPhone(box, id, comment.AuthorPhone)
		if err != nil {
			return false, err
		}
		updated, err := tx.ExecContext(ctx, "UPDATE said SET said_phone_cipher=?,said_phone_protected=true WHERE said_id=? AND said_phone IS NULL AND said_phone_protected=false", cipher, id)
		if err != nil {
			return false, err
		}
		n, err := updated.RowsAffected()
		if err != nil {
			return false, err
		}
		if n != 1 {
			return false, errors.New("comment phone protection update mismatch")
		}
	}
	return true, nil
}
