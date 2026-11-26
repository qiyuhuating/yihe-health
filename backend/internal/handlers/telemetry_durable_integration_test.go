package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryrepo"
)

func TestDurableTelemetryPersistsBeforeAckAndReplaysSafely(t *testing.T) {
	t.Setenv("YIHE_DURABLE_TELEMETRY", "1")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	var err error
	h.Box, err = securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"requestId": "durable-step-001", "step": "1200", "sleep": "8",
		"beTime": "2026-10-28 09:00:00",
	}
	requestStarted := time.Now().Add(-5 * time.Second)
	first := callAPI(h.deviceReport, payload)
	if first.Code != http.StatusOK {
		t.Fatalf("queue status: HTTP %d %s", first.Code, first.Body.String())
	}
	var ack map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &ack); err != nil || ack["queued"] != true {
		t.Fatalf("report is not durably acknowledged: %v: %s", err, first.Body.String())
	}
	var stored, projected int
	ctx := context.Background()
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM telemetry_inbox WHERE status='pending'").Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("durable queue before flush: %d: %v", stored, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM watch_day_info").Scan(&projected); err != nil || projected != 0 {
		t.Fatalf("projection occurred before queue flush: %d: %v", projected, err)
	}
	var lastTime, lastOnline *time.Time
	if err := pool.QueryRowContext(ctx, "SELECT last_time,last_online_at FROM device WHERE device_id=1").Scan(&lastTime, &lastOnline); err != nil {
		t.Fatal(err)
	}
	if lastTime == nil || lastOnline == nil {
		t.Fatalf("durably accepted report did not update device heartbeat: last_time=%v last_online_at=%v", lastTime, lastOnline)
	}
	if lastTime.Before(requestStarted) || lastOnline.Before(requestStarted) {
		t.Fatalf("durably accepted report left stale heartbeat timestamps: last_time=%s last_online_at=%s started=%s",
			lastTime.Format(time.RFC3339Nano), lastOnline.Format(time.RFC3339Nano), requestStarted.Format(time.RFC3339Nano))
	}
	replay := callAPI(h.deviceReport, payload)
	if replay.Code != http.StatusConflict {
		t.Fatalf("duplicate request not rejected: %d %s", replay.Code, replay.Body.String())
	}
	count, err := DrainDurableTelemetryBatch(ctx, pool, h.Box)
	if err != nil || count != 1 {
		t.Fatalf("drain: %d: %v", count, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM telemetry_inbox WHERE status='done'").Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("done row missing: %d: %v", stored, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM watch_day_info").Scan(&projected); err != nil || projected != 1 {
		t.Fatalf("daily projection missing: %d: %v", projected, err)
	}
	count, err = DrainDurableTelemetryBatch(ctx, pool, h.Box)
	if err != nil || count != 0 {
		t.Fatalf("second drain must be idempotent: %d: %v", count, err)
	}
}

func TestDurableTelemetryRetainsCorruptQueuePayload(t *testing.T) {
	t.Setenv("YIHE_DURABLE_TELEMETRY", "1")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	var err error
	h.Box, err = securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("m", 32))))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.ExecContext(context.Background(), `INSERT INTO telemetry_inbox(device_id,elder_id,binding_epoch,request_id,payload,status,created_at,next_retry_at)
		VALUES(?,?,?,?,?,'pending',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, 1, 1, 1, "corrupt-encrypted-payload", "not-an-envelope")
	if err != nil {
		t.Fatal(err)
	}
	count, err := DrainDurableTelemetryBatch(context.Background(), pool, h.Box)
	if err != nil || count != 1 {
		t.Fatalf("corrupt item drain should retain state: %d: %v", count, err)
	}
	var attempts int
	var status string
	if err := pool.QueryRowContext(context.Background(), "SELECT status,attempts FROM telemetry_inbox WHERE request_id='corrupt-encrypted-payload'").Scan(&status, &attempts); err != nil || status != "pending" || attempts != 1 {
		t.Fatalf("corrupt item was lost: status=%s attempts=%d err=%v", status, attempts, err)
	}
}

// Delayed inbox processing must never regress the device's last online time
// after a newer synchronous heartbeat.
func TestTouchDeviceTimeNeverRegresses(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	ctx := context.Background()
	var epoch int64
	if err := pool.QueryRowContext(ctx, "SELECT health_epoch FROM device WHERE device_id=?", 1).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	currentBattery, oldBattery := 80, 10
	if err := telemetryrepo.TouchDevice(ctx, pool, 1, epoch, &currentBattery, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := telemetryrepo.TouchDevice(ctx, pool, 1, epoch, &oldBattery, nil, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := telemetryrepo.TouchDevice(ctx, pool, 1, epoch+1, &oldBattery, nil, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var stored time.Time
	var battery int
	if err := pool.QueryRowContext(ctx, "SELECT last_time,battery FROM device WHERE device_id=?", 1).Scan(&stored, &battery); err != nil {
		t.Fatal(err)
	}
	if !stored.Equal(now) || battery != currentBattery {
		t.Fatalf("device state regressed: time=%v battery=%d; want %v / %d", stored, battery, now, currentBattery)
	}
}
