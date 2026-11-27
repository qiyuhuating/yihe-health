package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/observability"
)

func TestSOSAfterHealthCommitBeforeProjectionRelease(t *testing.T) {
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	reached, resume := make(chan struct{}), make(chan struct{})
	ctx := observability.WithTransactionTrace(t.Context(), func(stage string, _ time.Duration) {
		if stage == "post_commit_projection" {
			close(reached)
			<-resume
		}
	})
	body, err := json.Marshal(healthLockOrderBody("post-commit-sos"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.healthBatch(w, httptest.NewRequest("POST", "/api/v1/device/healthBatch", bytes.NewReader(body)).WithContext(ctx))
		done <- w
	}()
	defer func() {
		close(resume)
		select {
		case w := <-done:
			if w.Code != 200 {
				t.Errorf("health upload: %d %s", w.Code, w.Body.String())
			}
		case <-time.After(3 * time.Second):
			t.Error("health response did not complete")
		}
	}()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("health upload did not reach post-commit stage")
	}
	// Durable health data is visible, but the same owner is still reserved
	// against another upload until the projection publication finishes.
	var count int
	if err = pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM health_sample").Scan(&count); err != nil || count != 1 {
		t.Fatalf("health commit count=%d err=%v", count, err)
	}
	h.healthWrites.mu.Lock()
	_, ownerReserved := h.healthWrites.owners[1]
	_, deviceReserved := h.healthWrites.active["WATCH001"]
	h.healthWrites.mu.Unlock()
	if !ownerReserved || !deviceReserved {
		t.Fatal("upload reservation released too early")
	}
	sosBody, err := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "post-commit-sos"})
	if err != nil {
		t.Fatal(err)
	}
	sosCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	w := httptest.NewRecorder()
	start := time.Now()
	h.deviceSos(w, httptest.NewRequest("POST", "/api/v1/device/sos", bytes.NewReader(sosBody)).WithContext(sosCtx))
	var response map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response["returnCode"] != "1111" {
		t.Fatalf("SOS blocked by post-commit projection: %d %s", w.Code, w.Body.String())
	}
	if err = pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM alarm_precess").Scan(&count); err != nil || count != 1 {
		t.Fatalf("SOS commit count=%d err=%v", count, err)
	}
	t.Logf("SOS committed while health response remained held after durable commit: %v", time.Since(start))
}
