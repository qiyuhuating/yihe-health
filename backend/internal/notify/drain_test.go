package notify

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
	"yihe-care/internal/db/dbtest"
)

func TestWorkerDrainsReadyBacklogWithoutOneSecondPerMessage(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 120; i++ {
		if err = EnqueueChannels(ctx, tx, nil, "announcement", "burst", int64(i+1), "", []string{"in_app"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Current enqueue commits local inbox entries as available immediately;
	// construct the legacy backlog that still requires the compatibility worker.
	if _, err = p.ExecContext(ctx, "UPDATE message_outbox SET status='pending',delivered_at=NULL"); err != nil {
		t.Fatal(err)
	}
	worker, cancel := context.WithTimeout(ctx, 10*time.Second)
	started := time.Now()
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); Run(worker, p, nil) }()
	defer func() { cancel(); <-done }()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE status='delivered'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 120 {
			t.Logf("delivered 120 ready in-app notifications in %s", time.Since(started))
			return
		}
		select {
		case <-worker.Done():
			t.Fatalf("timer throttled a ready backlog: delivered %d/120", count)
		case <-tick.C:
		}
	}
}

func TestShortCycleLeavesExternalNotificationUnclaimed(t *testing.T) {
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	p := dbtest.MySQL(t)
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = EnqueueChannels(ctx, tx, nil, "announcement", "budget", 1, "13800000001", []string{"sms"}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(202) }))
	defer gateway.Close()
	client := gateway.Client()
	client.Timeout = 2 * time.Second
	worker := Worker{DB: p, URL: gateway.URL, Token: "test", Client: client}
	short, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err = worker.Tick(short); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err = p.QueryRowContext(ctx, "SELECT status,attempts FROM message_outbox").Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || status != "pending" || attempts != 0 {
		t.Fatal("short cycle took a lease it could not safely complete", calls.Load(), status, attempts)
	}
}
