package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"yihe-care/internal/health"
)

// Different device numbers bound to one resident must not occupy two
// transaction connections while their common revision clock is blocked.
func TestOwnerDomainLeavesConnectionForOtherResident(t *testing.T) {
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'second resident')")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT 2,'WATCH002',secret,1 FROM device WHERE device_id=1")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT 3,'WATCH003',secret,2 FROM device WHERE device_id=1")
	pool.SetMaxOpenConns(3)
	pool.SetMaxIdleConns(3)
	h := testHandler(t, pool)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	unlock := holdHealthOwnerClock(t, ctx, pool)
	defer unlock()
	body := func(device, label string) map[string]any {
		b := healthLockOrderBody(label)
		b["deviceNo"] = device
		b["samples"].([]health.Sample)[0].SourceDevice = device
		return b
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- callAPI(h.healthBatch, body("WATCH001", "first")) }()
	if err := waitHealthClockContention(ctx, pool); err != nil {
		t.Fatal(err)
	}
	second := make(chan *httptest.ResponseRecorder, 1)
	go func() { second <- callAPI(h.healthBatch, body("WATCH002", "second")) }()
	// The old implementation fills the last connection with the second
	// blocked transaction. The new one reaches the local conflict queue.
	deadline := time.Now().Add(150 * time.Millisecond)
	for len(h.healthWrites.waiting) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(h.healthWrites.waiting) == 0 {
		unlock()
		<-first
		<-second
		t.Fatal("same owner consumed another transaction connection instead of local queue")
	}
	if got := pool.Stats().InUse; got != 2 {
		t.Fatalf("queued owner holds connection: in-use=%d", got)
	}
	other := make(chan *httptest.ResponseRecorder, 1)
	start := time.Now()
	go func() { other <- callAPI(h.healthBatch, body("WATCH003", "other")) }()
	select {
	case w := <-other:
		if w.Code != http.StatusOK {
			t.Fatalf("other resident %d %s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("other resident blocked")
	}
	t.Logf("unrelated resident committed while first owner blocked: %s", time.Since(start))
	select {
	case w := <-second:
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
			t.Fatalf("same owner response %d %s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("same owner did not obey queue deadline")
	}
	unlock()
	select {
	case w := <-first:
		if w.Code != http.StatusOK {
			t.Fatalf("first response %d %s", w.Code, w.Body)
		}
	case <-ctx.Done():
		t.Fatal("first request did not finish")
	}
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample").Scan(&n); err != nil || n != 2 {
		t.Fatalf("durable rows=%d err=%v", n, err)
	}
}
