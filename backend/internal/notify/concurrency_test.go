package notify

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestDrainConcurrencyIsBounded(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"", 1}, {"invalid", 1}, {"0", 1}, {"-1", 1}, {"9", 1}, {"1", 1}, {"4", 4}, {"8", 8},
	} {
		t.Setenv("NOTIFY_DRAIN_CONCURRENCY", tc.value)
		if got := drainConcurrency(); got != tc.want {
			t.Fatalf("configured=%q, got %d workers, want %d", tc.value, got, tc.want)
		}
	}
}

func TestIndependentDrainWorkersCanProgressWhileProviderBlocks(t *testing.T) {
	t.Setenv("NOTIFY_DRAIN_CONCURRENCY", "2")
	t.Setenv("TENCENT_SMS_APPID", "")
	t.Setenv("NOTIFY_ALLOW_HTTP_TEST", "1")
	t.Setenv("NOTIFY_BULK_WEBHOOK_URL", "")
	p := dbtest.MySQL(t)
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 2; i++ {
		err = EnqueueChannels(ctx, tx, nil, "announcement", fmt.Sprintf("parallel-%d", i), 1, "13800000001", []string{"sms"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	arrivals := make(chan struct{}, 2)
	release := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrivals <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer gateway.Close()
	defer close(release)
	t.Setenv("NOTIFY_WEBHOOK_URL", gateway.URL)
	t.Setenv("NOTIFY_WEBHOOK_TOKEN", "test-token")

	parent, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); Run(parent, p, nil) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("drain worker shutdown exceeded 5 seconds")
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-arrivals:
		case <-time.After(5 * time.Second):
			t.Fatal("external delivery serialized behind one blocked provider request")
		}
	}
	var claimed int
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM message_outbox WHERE status='sending' AND attempts=1").Scan(&claimed); err != nil || claimed != 2 {
		t.Fatalf("claimed=%d, error=%v", claimed, err)
	}
}
