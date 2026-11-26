package db_test

import (
	"context"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func TestMySQLQueryPreservesLiteralsWithNativeRegex(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var matched int
	var literal string
	var now time.Time
	err := pool.QueryRowContext(ctx, "SELECT ? REGEXP ?, 'now() ~ $9', CURRENT_TIMESTAMP(6)", "12345", "^123").Scan(&matched, &literal, &now)
	if err != nil {
		t.Fatal(err)
	}
	if matched != 1 || literal != "now() ~ $9" || now.IsZero() {
		t.Fatalf("unexpected native-regex result: matched=%d literal=%q now=%v", matched, literal, now)
	}
}

func TestMySQLNativeDuplicateSafeInsert(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := pool.ExecContext(ctx, "INSERT INTO watch_info(device_id) VALUES(?) ON DUPLICATE KEY UPDATE device_id=device_id", 987654); err != nil {
			t.Fatalf("duplicate-safe insert %d failed: %v", i, err)
		}
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM watch_info WHERE device_id=?", 987654).Scan(&count); err != nil || count != 1 {
		t.Fatalf("native MySQL idempotency failed: count=%d err=%v", count, err)
	}
}

func TestInsertIDSupportsGeneratedKeys(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	alarmID, err := db.InsertID(ctx, pool, "INSERT INTO alarm_precess(status) VALUES(?)", "1")
	if err != nil || alarmID <= 0 {
		t.Fatalf("alarm InsertID failed: id=%d err=%v", alarmID, err)
	}
	textID, err := db.InsertID(ctx, pool, "INSERT INTO `text`(title,text_main,time) VALUES(?,?,?)",
		"native insert-id test", "body", time.Now())
	if err != nil || textID <= 0 {
		t.Fatalf("text InsertID failed: id=%d err=%v", textID, err)
	}
}
