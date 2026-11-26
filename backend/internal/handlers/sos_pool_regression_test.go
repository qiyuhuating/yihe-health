package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSOSReservedPoolUnderBusinessSaturation(t *testing.T) {
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	if pool.SOSPool() == pool.DB || pool.SOSPool().Stats().MaxOpenConnections != 2 {
		t.Fatal("SOS budget not isolated")
	}
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	body, err := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "isolated-pool-press", "bindingVersion": 1})
	if err != nil {
		t.Fatal(err)
	}
	var prior any
	for i := range 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/device/sos", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		h.deviceSos(w, req)
		cancel()
		if w.Code != http.StatusOK {
			t.Fatalf("SOS blocked by business pool: HTTP %d %s", w.Code, w.Body)
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["returnCode"] != "1111" {
			t.Fatal(result)
		}
		if i == 0 {
			prior = result["incidentId"]
		} else if result["duplicate"] != true || result["incidentId"] != prior {
			t.Fatal("SOS retry changed idempotency", result)
		}
	}
	var n int
	if err := pool.SOSPool().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM alarm_precess").Scan(&n); err != nil || n != 1 {
		t.Fatal("expected one durable incident", n, err)
	}
}
