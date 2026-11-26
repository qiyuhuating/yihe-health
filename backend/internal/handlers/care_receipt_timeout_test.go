package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// A timeout can occur AFTER the gateway accepted the request. An authenticated
// receipt must still be reflected in durable care state, without faking delivery
// for a task that was never dispatched.
func TestCareDelayedReceiptAfterGatewayTimeout(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'家属','13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)
	api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "test-press", "bindingVersion": 1})
	ctx := context.Background()
	var id int64
	if err := pool.QueryRowContext(ctx, "SELECT id FROM care_notification_outbox").Scan(&id); err != nil {
		t.Fatal(err)
	}
	secret := "delayed-gateway-test-key-at-least-32-characters"
	t.Setenv("CARE_RECEIPT_SECRET", secret)
	body, _ := json.Marshal(map[string]any{"notificationId": id, "status": "delivered"})
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/care/receipt", bytes.NewReader(body))
		req.Header.Set("X-Care-Timestamp", stamp)
		req.Header.Set("X-Care-Signature", hex.EncodeToString(mac.Sum(nil)))
		w := httptest.NewRecorder()
		h.careReceipt(w, req)
		return w
	}
	if rec := send(); rec.Code != http.StatusConflict {
		t.Fatalf("signed receipt for never dispatched task was accepted: %d %s", rec.Code, rec.Body.String())
	}
	mustExec(t, pool, "UPDATE care_notification_outbox SET status='failed',attempts=1,last_attempt_at=CURRENT_TIMESTAMP(6) WHERE id=?", id)
	for range 2 {
		if rec := send(); rec.Code != http.StatusOK {
			t.Fatalf("signed delayed or retried delivery was rejected: %d %s", rec.Code, rec.Body.String())
		}
	}
	var status, incident string
	var events int
	if err := pool.QueryRowContext(ctx, "SELECT status FROM care_notification_outbox WHERE id=?", id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT state FROM care_incident").Scan(&incident); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident_event WHERE action='delivered'").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if status != "delivered" || incident != "notified" || events != 1 {
		t.Fatalf("late delivery state mismatch: status=%s incident=%s events=%d", status, incident, events)
	}
}
