package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestStaleTelemetryReturnsEpochExpiredWithoutPersistence(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	mustExec(t, pool, "UPDATE device SET old_id=1 WHERE device_id=1")
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		body    map[string]any
	}{
		{"report", h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "hr": 74}},
		{"batch", h.healthBatch, healthLockOrderBody("expired-batch")},
		{"batch_owner_first", func(w http.ResponseWriter, r *http.Request) { h.healthBatchWithLockOrder(w, r, true) }, healthLockOrderBody("expired-owner-first")},
		{"sos", h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "expired-epoch-sos"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := func(want int, code string) {
				t.Helper()
				r := callAPI(tc.handler, tc.body)
				var reply struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(r.Body.Bytes(), &reply); err != nil || r.Code != want || reply.Code != code {
					t.Fatalf("HTTP %d body=%s, want HTTP %d code=%s: %v", r.Code, r.Body.String(), want, code, err)
				}
			}
			check(http.StatusConflict, "EPOCH_EXPIRED")
			tc.body["bindingVersion"] = 4
			check(http.StatusConflict, "BINDING_CHANGED")
			tc.body["bindingVersion"], tc.body["secret"] = 1, "incorrect"
			check(http.StatusUnauthorized, "DEVICE_AUTH_FAILED")
		})
	}
	for _, table := range []string{"ratepre", "health_sample", "health_change", "telemetry_inbox", "care_incident"} {
		var count int
		if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("rejected epoch wrote %s: count=%d err=%v", table, count, err)
		}
	}
}
