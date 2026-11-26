package notify

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueueHasNoHundredMessageDropAndRollsBack(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	box, _ := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if err = EnqueueChannels(ctx, tx, box, "announcement", strconv.Itoa(i), 1, "13800000001", []string{"sms", "in_app"}); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox").Scan(&count); err != nil || count != 240 {
		t.Fatal(count, err)
	}
	tx, _ = pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	EnqueueChannels(ctx, tx, box, "announcement", "rollback", 1, "13800000001", []string{"sms", "in_app"})
	tx.Rollback()
	if err = pool.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE source_key='rollback'").Scan(&count); err != nil || count != 0 {
		t.Fatal("rolled-back announcement leaked notification", count, err)
	}
	var recipient string
	pool.QueryRowContext(ctx, "SELECT recipient FROM message_outbox LIMIT 1").Scan(&recipient)
	if recipient == "13800000001" {
		t.Fatal("recipient plaintext")
	}
}
func TestRetryExhaustionFallsBackAndInAppDoesNotNeedGateway(t *testing.T) {
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	tx, _ := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err := EnqueueChannels(ctx, tx, nil, "announcement", "one", 1, "13800000001", []string{"sms"}); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing deduplication key")
		}
		w.WriteHeader(503)
	}))
	defer server.Close()
	d := Worker{DB: pool, URL: server.URL, Token: "test", Client: server.Client()}
	for range 5 {
		pool.ExecContext(ctx, "UPDATE message_outbox SET retry_at=? WHERE channel='sms'", time.Now().Add(-time.Minute))
		if err := d.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var status string
	var count int
	pool.QueryRowContext(ctx, "SELECT status FROM message_outbox WHERE channel='sms'").Scan(&status)
	pool.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE channel='voice'").Scan(&count)
	if status != "exhausted" || count != 1 {
		t.Fatal(status, count)
	}
	tx, _ = pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	EnqueueChannels(ctx, tx, nil, "announcement", "local", 1, "13800000001", []string{"in_app"})
	tx.Commit()
	pool.ExecContext(ctx, "UPDATE message_outbox SET retry_at=? WHERE channel='voice'", time.Now().Add(time.Hour))
	if err := d.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	pool.QueryRowContext(ctx, "SELECT status FROM message_outbox WHERE channel='in_app'").Scan(&status)
	if status != "delivered" {
		t.Fatal(status)
	}
}
func TestStaleLeaseCannotScheduleSpuriousFallback(t *testing.T) {
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	tx, _ := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	EnqueueChannels(ctx, tx, nil, "announcement", "race", 1, "13800000001", []string{"sms"})
	tx.Commit()
	pool.ExecContext(ctx, "UPDATE message_outbox SET attempts=4")
	arrived, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(arrived)
			<-release
			w.WriteHeader(503)
		} else {
			w.WriteHeader(202)
		}
	}))
	defer server.Close()
	d := Worker{DB: pool, URL: server.URL, Token: "test", Client: server.Client()}
	done := make(chan error, 1)
	go func() { done <- d.Tick(ctx) }()
	<-arrived
	pool.ExecContext(ctx, "UPDATE message_outbox SET last_attempt_at=?", time.Now().Add(-2*time.Minute))
	if err := d.Tick(ctx); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var state string
	var fallback int
	pool.QueryRowContext(ctx, "SELECT status FROM message_outbox WHERE channel='sms'").Scan(&state)
	pool.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE channel='voice'").Scan(&fallback)
	if state != "sent" || fallback != 0 {
		t.Fatal(state, fallback)
	}
}
