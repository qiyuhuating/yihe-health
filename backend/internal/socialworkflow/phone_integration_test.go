package socialworkflow

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/socialrepo"
)

func TestSocialPhoneImageRollbackMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("K", 32))))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO family(family_id,family_phone,family_tou_pic) VALUES(1,'13800000001','owned-avatar')"); err != nil {
		t.Fatal(err)
	}
	comment := socialrepo.NewComment{NoticeID: 1, AuthorPhone: "13800000001", Content: "synthetic", CreatedAt: time.Now()}
	called := false
	// Force the protection update to fail after avatar ownership was locked and
	// the comment ID reserved. The caller transaction must undo both operations.
	if _, err := pool.ExecContext(ctx, `CREATE TRIGGER reject_comment_phone BEFORE UPDATE ON said FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic failure'`); err != nil {
		t.Fatal(err)
	}
	locker := func(ctx context.Context, tx *sql.Tx, names []string) error {
		called = true
		if len(names) != 1 || names[0] != "owned-avatar" {
			return errors.New("avatar ownership changed")
		}
		_, err := tx.ExecContext(ctx, "UPDATE family SET family_name='must roll back' WHERE family_id=1")
		return err
	}
	if created, err := CreateComment(ctx, pool, box, locker, comment); err == nil || created || !called {
		t.Fatal("protection failure did not reach the caller transaction", err)
	}
	var rows int
	var name *string
	if err := pool.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM said)+(SELECT COUNT(*) FROM social_phone_key_state),family_name FROM family WHERE family_id=1").Scan(&rows, &name); err != nil || rows != 0 || name != nil {
		t.Fatal("failed protection left comment, binding or image transaction effects", err)
	}
	if _, err := pool.ExecContext(ctx, "DROP TRIGGER reject_comment_phone"); err != nil {
		t.Fatal(err)
	}
	if created, err := CreateComment(ctx, pool, box, locker, comment); err != nil || !created {
		t.Fatal("comment could not recover with the original avatar transaction", err)
	}
}
