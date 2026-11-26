package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"yihe-care/internal/config"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/ratelimit"
	"yihe-care/internal/securestore"
)

func TestLoginAuditBoundarySharedAcrossReplicasAndIPs(t *testing.T) {
	pool := testDatabase(t)
	cfg := config.Config{DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), RateLimit: ratelimit.Options{TrustedProxyCIDRs: []string{"10.0.0.0/8"}}}
	a, b := New(pool, nil, cfg), New(pool, nil, cfg)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:42"
	r.Header.Set("X-Forwarded-For", "192.0.2.1")
	for range 10 {
		if err := a.loginAudit(r, "13800000001", "", 0, "invalid_credentials"); err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	if b.loginAllowed(w, r, "13800000001") || w.Code != 429 {
		t.Fatal("another replica ignored durable failures")
	}
	r.Header.Set("X-Forwarded-For", "192.0.2.2")
	w = httptest.NewRecorder()
	if !b.loginAllowed(w, r, "13800000001") {
		t.Fatal("paired-IP accounting is not using trusted client address")
	}
	for i := 0; i < 20; i++ {
		r.Header.Set("X-Forwarded-For", "192.0.2.1")
		r.RemoteAddr = fmt.Sprintf("198.51.100.%d:42", i+1)
		if err := a.loginAudit(r, "13800000001", "", 0, "invalid_credentials"); err != nil {
			t.Fatal(err)
		}
	}
	r.RemoteAddr = "203.0.113.99:42"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	w = httptest.NewRecorder()
	if b.loginAllowed(w, r, "13800000001") || w.Code != 429 {
		t.Fatal("rotating addresses bypassed shared account boundary")
	}
}

func TestWrongCoordinateKeyFailsClosed(t *testing.T) {
	a, _ := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	b, _ := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)))
	sealed, err := a.Seal("113.123", coordinateScope)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := openCoordinate(b, &sealed); err == nil || value != nil {
		t.Fatal("wrong key disclosed ciphertext as a coordinate")
	}
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "UPDATE device SET last_lng=?,last_lat=? WHERE device_id=1", sealed, sealed)
	h := testHandler(t, pool)
	h.Box = b
	if _, err = h.readDeviceRecord(devicerepo.FindDeviceByNo(context.Background(), pool, "WATCH001")); err == nil {
		t.Fatal("device read ignored unavailable coordinate key")
	}
}
