package handlers

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCareReceiptAcceptsFileBackedSigningSecret(t *testing.T) {
	secret := "care-test-strong-secret-at-least-32-bytes"
	keyfile := filepath.Join(t.TempDir(), "care-secret")
	if err := os.WriteFile(keyfile, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARE_RECEIPT_SECRET", "")
	t.Setenv("CARE_RECEIPT_SECRET_FILE", keyfile)
	body := []byte(`{"notificationId":42,"status":"delivered"}`)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stamp + "."))
	_, _ = mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/care/receipt", bytes.NewReader(body))
	req.Header.Set("X-Care-Timestamp", stamp)
	req.Header.Set("X-Care-Signature", hex.EncodeToString(mac.Sum(nil)))
	rec := httptest.NewRecorder()
	item, ok := (&Handler{}).decodeCareReceipt(rec, req)
	if !ok || item.NotificationID != 42 || item.Status != "delivered" {
		t.Fatalf("file-backed receipt rejected: code=%d body=%s", rec.Code, rec.Body.String())
	}
	t.Setenv("CARE_RECEIPT_SECRET_FILE", keyfile+".missing")
	req = httptest.NewRequest(http.MethodPost, "/api/v1/care/receipt", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	if _, ok := (&Handler{}).decodeCareReceipt(rec, req); ok || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing secret file should fail closed: %d", rec.Code)
	}
}
