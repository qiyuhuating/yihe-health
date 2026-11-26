package care

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	dbstore "yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func testCareDB(t *testing.T) *dbstore.Database {
	t.Helper()
	return seedCareDB(t, dbtest.MySQL(t))
}

func seedCareDB(t *testing.T, pool *dbstore.Database) *dbstore.Database {
	ctx := context.Background()
	for _, sql := range []string{
		"INSERT INTO oldperson(old_id,name) VALUES(1,'长者')",
		"INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'TEST001','unused',1)",
		"INSERT INTO care_incident(id,old_id,device_id,due_at) VALUES(1,1,1,CURRENT_TIMESTAMP(6)+INTERVAL 5 MINUTE)",
	} {
		if _, err := pool.ExecContext(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func TestOutboxDispatchIsRecordedButNotFalselyDelivered(t *testing.T) {
	pool := testCareDB(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "INSERT INTO care_notification_outbox(incident_id,channel,recipient) VALUES(1,'sms','13800000001')"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing gateway credentials or idempotency key")
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	d := &dispatcher{db: pool, webhookURL: server.URL, webhookToken: "test-token", httpClient: &http.Client{Timeout: time.Second}}
	if _, err := d.dispatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.dispatchNext(ctx); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("dispatch count %d", calls)
	}
	var status, incident string
	if err := pool.QueryRowContext(ctx, "SELECT status FROM care_notification_outbox").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT state FROM care_incident WHERE id=1").Scan(&incident); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || incident != "received" {
		t.Fatalf("HTTP 202 is not delivery: outbox=%s incident=%s", status, incident)
	}
}

func TestOverdueSOSCreatesEscalationAuditAndFallback(t *testing.T) {
	pool := testCareDB(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "UPDATE care_incident SET due_at=CURRENT_TIMESTAMP(6)-INTERVAL 1 MINUTE WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	d := &dispatcher{db: pool, escalationPhone: "13800000002"}
	if err := d.escalate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := d.escalate(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	var audits, notices int
	if err := pool.QueryRowContext(ctx, "SELECT state FROM care_incident WHERE id=1").Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_incident_event WHERE action='escalate'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_notification_outbox").Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if state != "escalated" || audits != 1 || notices != 1 {
		t.Fatal(fmt.Sprintf("state=%s audits=%d notices=%d", state, audits, notices))
	}
}

// This test exercises the ABA race when an expired claim is re-leased while
// the original HTTP request is still pending: only the latest attempt may write.
func TestStaleNotificationAttemptCannotOverrideNewClaim(t *testing.T) {
	pool := testCareDB(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "INSERT INTO care_notification_outbox(incident_id,channel,recipient) VALUES(1,'sms','13800000001')"); err != nil {
		t.Fatal(err)
	}
	firstArrived := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(firstArrived)
			<-releaseFirst
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer httpServer.Close()
	d := &dispatcher{db: pool, webhookURL: httpServer.URL, webhookToken: "test-token", httpClient: &http.Client{Timeout: 10 * time.Second}}
	done := make(chan error, 1)
	go func() { _, err := d.dispatchNext(ctx); done <- err }()
	select {
	case <-firstArrived:
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		t.Fatal("first delivery was not claimed")
	}
	// Simulate an expired lease without waiting for two minutes.
	if _, err := pool.ExecContext(ctx, "UPDATE care_notification_outbox SET last_attempt_at=CURRENT_TIMESTAMP(6)-INTERVAL 3 MINUTE"); err != nil {
		close(releaseFirst)
		t.Fatal(err)
	}
	if _, err := d.dispatchNext(ctx); err != nil {
		close(releaseFirst)
		t.Fatal(err)
	}
	close(releaseFirst)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err := pool.QueryRowContext(ctx, "SELECT status,attempts FROM care_notification_outbox").Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || attempts != 2 {
		t.Fatalf("stale writer corrupted latest attempt: status=%s attempts=%d", status, attempts)
	}
}

func TestOfflineDeviceScanSkipsBusyDevice(t *testing.T) {
	pool := testCareDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := pool.ExecContext(ctx, `UPDATE device SET last_time=CURRENT_TIMESTAMP(6)-INTERVAL 5 MINUTE,last_online_at=CURRENT_TIMESTAMP(6)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, `INSERT INTO device(device_id,device_no,secret,last_time,last_online_at) VALUES(2,'OFFLINE2','unused',CURRENT_TIMESTAMP(6)-INTERVAL 5 MINUTE,CURRENT_TIMESTAMP(6))`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE device SET battery=50 WHERE device_id=1"); err != nil {
		t.Fatal(err)
	}
	scan, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	if err := scanOffline(scan, pool); err != nil {
		t.Fatalf("offline scan waited behind a device transaction: %v", err)
	}
	var first, second int
	if err := pool.QueryRowContext(ctx, "SELECT boot_seq FROM device WHERE device_id=1").Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT boot_seq FROM device WHERE device_id=2").Scan(&second); err != nil || second != first+1 {
		t.Fatalf("unlocked offline device was not processed: first=%d second=%d err=%v", first, second, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := scanOffline(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT boot_seq FROM device WHERE device_id=1").Scan(&first); err != nil || first != second {
		t.Fatalf("skipped device was not processed on the next scan: %d %v", first, err)
	}
}

func TestOfflineDeviceScanPages(t *testing.T) {
	pool := testCareDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for id := 2; id <= 205; id++ {
		if _, err := pool.ExecContext(ctx, "INSERT INTO device(device_id,device_no,secret) VALUES(?,?,'unused')", id, fmt.Sprint("PAGE", id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.ExecContext(ctx, "UPDATE device SET last_time=CURRENT_TIMESTAMP(6)-INTERVAL 5 MINUTE,last_online_at=CURRENT_TIMESTAMP(6),boot_seq=1"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := scanOffline(ctx, pool); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM device WHERE boot_seq=2 AND last_online_at IS NULL").Scan(&count); err != nil || count != 205 {
			t.Fatalf("offline pages incomplete or repeated: count=%d err=%v", count, err)
		}
	}
}

func TestOfflineDeviceScanRunsOnceUntilNextOnlineSession(t *testing.T) {
	pool := testCareDB(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, `UPDATE device SET last_time=CURRENT_TIMESTAMP(6)-INTERVAL 3 MINUTE,
		last_online_at=CURRENT_TIMESTAMP(6)-INTERVAL 3 MINUTE,boot_seq=2,person_handled=1 WHERE device_id=1`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := scanOffline(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var boot, handled int
	var online *time.Time
	if err := pool.QueryRowContext(ctx, "SELECT boot_seq,person_handled,last_online_at FROM device WHERE device_id=1").Scan(&boot, &handled, &online); err != nil {
		t.Fatal(err)
	}
	if boot != 3 || handled != 0 || online != nil {
		t.Fatalf("offline scan must only alert once: boot=%d handled=%d online=%v", boot, handled, online)
	}
}

func TestRetryDelayIsCapped(t *testing.T) {
	for _, tt := range []struct {
		attempts int
		want     time.Duration
	}{
		{1, 30 * time.Second}, {2, time.Minute}, {3, 2 * time.Minute},
		{7, 32 * time.Minute}, {1000, 32 * time.Minute},
	} {
		if actual := retryDelay(tt.attempts); actual != tt.want {
			t.Fatalf("attempt %d: delay=%v, want=%v", tt.attempts, actual, tt.want)
		}
	}
}
