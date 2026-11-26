package rekey

import (
	"context"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/socialrepo"
)

func TestSocialPhoneRekeyMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	old, current, retired, _ := boxes(t)
	const phone = "13800000001"
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if created, err := socialrepo.InsertComment(ctx, tx, old, socialrepo.NewComment{NoticeID: 1, AuthorPhone: phone, Content: "synthetic", CreatedAt: time.Now()}); err != nil || !created {
		t.Fatal("actual comment writer failed", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := socialrepo.ToggleLike(ctx, pool, old, 1, phone); err != nil {
		t.Fatal("actual vote writer failed", err)
	}
	var before string
	if err := pool.QueryRowContext(ctx, "SELECT said_phone_cipher FROM said WHERE said_id=1").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if report, err := Run(ctx, pool.DB, current, old, Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 1000}); err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized {
		t.Fatal("rotation failed or authorized unsupported key retirement", err)
	}
	items, err := socialrepo.ListComments(ctx, pool, retired, phone, 1)
	if err != nil || len(items) != 1 || items[0].SaidPhone == nil || *items[0].SaidPhone != "138****0001" || items[0].Good == nil || *items[0].Good != "1" {
		t.Fatal("production reader still needs old encryption key", err)
	}
	if _, err := socialrepo.ListComments(ctx, pool, old, phone, 1); err == nil {
		t.Fatal("old-only reader unexpectedly opened rotated comment")
	}
	if _, err := retired.Open(before, "social-comment-phone:1"); err == nil {
		t.Fatal("retired reader still opens preserved historical ciphertext")
	}
	if _, err := socialrepo.ToggleLike(ctx, pool, retired, 1, phone); err != nil {
		t.Fatal("AEAD rotation changed stable vote identity", err)
	}
}
