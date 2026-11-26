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

func TestCareImmediateSignedReceiptDuringSending(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'家属','13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)
	api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "test-press", "bindingVersion": 1})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id int64
	mustExec(t, pool, "UPDATE care_notification_outbox SET status='sending'")
	if err := pool.QueryRowContext(ctx, "SELECT id FROM care_notification_outbox").Scan(&id); err != nil {
		t.Fatal(err)
	}
	secret := "test-receipt-secret-at-least-32-characters"
	t.Setenv("CARE_RECEIPT_SECRET", secret)
	body, _ := json.Marshal(map[string]any{"notificationId": id, "status": "delivered"})
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(stamp + "."))
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/care/receipt", bytes.NewReader(body))
	req.Header.Set("X-Care-Timestamp", stamp)
	req.Header.Set("X-Care-Signature", hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	h.careReceipt(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("immediate signed receipt: %d %s", rec.Code, rec.Body.String())
	}
	var state string
	if err := pool.QueryRowContext(ctx, "SELECT status FROM care_notification_outbox WHERE id=?", id).Scan(&state); err != nil || state != "delivered" {
		t.Fatalf("delivery lost: %s, %v", state, err)
	}
}
