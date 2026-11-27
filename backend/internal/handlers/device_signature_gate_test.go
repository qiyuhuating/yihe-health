package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveDeviceGate(h *Handler, req *http.Request) *httptest.ResponseRecorder {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("reached"))
	})
	w := httptest.NewRecorder()
	h.EnforceDeviceSignatures(next).ServeHTTP(w, req)
	return w
}

func gateRequest(t *testing.T, path string, body map[string]any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
}

// Without a database the gate can still prove its routing and opt-in policy:
// every path that reaches hwAuth is rejected before any DB work is attempted.
func TestDeviceSignatureGateScopeWithoutDatabase(t *testing.T) {
	body := map[string]any{"deviceNo": "WATCH001", "secret": testSecret}
	cases := []struct {
		name    string
		path    string
		require bool
		want    int
	}{
		{"shared route untouched", "/api/v1/health/statistics", true, http.StatusOK},
		{"report keeps its transactional nonce", "/api/v1/device/report", true, http.StatusOK},
		{"sos keeps its transactional nonce", "/api/v1/device/sos", true, http.StatusOK},
		{"care receipt uses gateway HMAC", "/api/v1/care/receipt", true, http.StatusOK},
		{"device route unsigned while disabled", "/api/v1/device/heartbeat", false, http.StatusOK},
		{"device route unsigned while required", "/api/v1/device/heartbeat", true, http.StatusUnauthorized},
		{"device route unsigned login while required", "/api/v1/device/login", true, http.StatusUnauthorized},
		{"device route unsigned bind while required", "/api/v1/device/bind", true, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.require {
				t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "1")
			} else {
				t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "")
			}
			w := serveDeviceGate(&Handler{}, gateRequest(t, tc.path, body))
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want == http.StatusOK && w.Body.String() != "reached" {
				t.Fatalf("downstream handler not invoked: %q", w.Body.String())
			}
		})
	}
}

// A supplied signature must verify even when the opt-in switch is off.
func TestDeviceSignatureGateRejectsInvalidSignatureWhenDisabled(t *testing.T) {
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "")
	body := map[string]any{"deviceNo": "WATCH001", "secret": testSecret}
	req := signedTestRequest(t, "/api/v1/device/heartbeat", body, "heartbeat-invalid-0123456789", time.Now())
	req.Header.Set("X-Device-Signature", "not-hex")
	w := serveDeviceGate(&Handler{}, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature accepted: %d %s", w.Code, w.Body.String())
	}
	req = signedTestRequest(t, "/api/v1/device/heartbeat", body, "heartbeat-expired-0123456789", time.Now().Add(-20*time.Minute))
	w = serveDeviceGate(&Handler{}, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired signature accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestDeviceSignatureGateRejectsNonceReplay(t *testing.T) {
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "1")
	db := testDatabase(t)
	seedDevice(t, db)
	h := testHandler(t, db)
	body := map[string]any{"deviceNo": "WATCH001", "secret": testSecret}
	nonce := "heartbeat-replay-0123456789"
	expected, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(raw, expected) {
			t.Fatal("verified request body changed")
		}
		_, _ = w.Write([]byte("reached"))
	})
	h.EnforceDeviceSignatures(next).ServeHTTP(first, signedTestRequest(t, "/api/v1/device/heartbeat", body, nonce, time.Now()))
	if first.Code != http.StatusOK {
		t.Fatalf("signed heartbeat rejected: %d %s", first.Code, first.Body.String())
	}
	second := serveDeviceGate(h, signedTestRequest(t, "/api/v1/device/heartbeat", body, nonce, time.Now()))
	if second.Code != http.StatusConflict {
		t.Fatalf("nonce replay accepted: %d %s", second.Code, second.Body.String())
	}
}
