//go:build yihecore && cgo && !purego && ((windows && amd64) || (linux && (amd64 || arm64)))

package handlers

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

// A committed health transaction must not keep its admission slot while a
// derived cache update is blocked. This test holds a real shard mutex, observes
// the committed owner clock from another SQL connection, then probes admission.
func TestCommittedUploadReleasesBeforeProjection(t *testing.T) {
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	h := New(pool, store, config.Config{HealthCacheBytes: 8 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	api(t, h.healthBatch, healthLockOrderBody("seed"))
	before, err := health.StatisticsRevision(t.Context(), pool, 1)
	if err != nil || before <= 0 {
		t.Fatalf("seed revision=%d err=%v", before, err)
	}
	key := coreKey{1, "watch", "WATCH001"}
	shard := h.healthCache.core.shard(key)
	done := make(chan *httptest.ResponseRecorder, 1)
	shard.mu.Lock()
	released := false
	defer func() {
		if !released {
			shard.mu.Unlock()
			<-done
		}
	}()
	go func() { done <- callAPI(h.healthBatch, healthLockOrderBody("next")) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		rev, err := health.StatisticsRevision(t.Context(), pool, 1)
		if err != nil {
			t.Fatal(err)
		}
		if rev > before {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("upload did not commit")
		}
		time.Sleep(time.Millisecond)
	}
	// The handler cannot have returned: its post-commit cache lookup is blocked.
	select {
	case <-done:
		shard.mu.Unlock()
		released = true
		t.Fatal("projection did not block")
	default:
	}
	start := time.Now()
	if !h.healthWrites.acquire(t.Context(), "WATCH001") {
		t.Fatal("committed transaction retained admission during projection")
	}
	h.healthWrites.release("WATCH001")
	t.Logf("same-device admission while projection blocked: %s", time.Since(start))
	shard.mu.Unlock()
	released = true
	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("committed upload: %d %s", w.Code, w.Body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not finish")
	}
}
