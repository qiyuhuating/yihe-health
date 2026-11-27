package handlers

import (
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/devicecredential"
	"yihe-care/internal/health"
)

func TestHealthDeviceHintRevalidatesCurrentBinding(t *testing.T) {
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	t.Setenv("YIHE_SOS_PRIORITY", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	h := testHandler(t, pool)
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "hint", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	req := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}}
	write := func() {
		t.Helper()
		out := api(t, h.healthBatch, req)
		if out["returnCode"] != "1111" {
			t.Fatal(out)
		}
	}
	write()
	selects := func() int64 {
		t.Helper()
		var name string
		var n int64
		if err := pool.QueryRowContext(t.Context(), "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := selects()
	write()
	hit := selects() - before
	h.healthHints.remove("WATCH001")
	before = selects()
	write()
	miss := selects() - before
	if miss != hit+1 {
		t.Fatalf("warm SELECTs=%d cold=%d", hit, miss)
	}
	t.Logf("binding hint duplicate: SELECTs %d -> %d", miss, hit)

	const rotated = "rotated-device-secret-1234567890"
	hash, err := devicecredential.Hash(rotated)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, "UPDATE device SET secret=? WHERE device_id=1", hash)
	s.Version = 2
	req["samples"] = []health.Sample{s}
	if w := callAPI(h.healthBatch, req); w.Code != http.StatusUnauthorized {
		t.Fatalf("rotated credential accepted: %d %s", w.Code, w.Body)
	}
	req["secret"] = rotated
	write()
	mustExec(t, pool, "UPDATE device SET status=0 WHERE device_id=1")
	s.Version = 3
	req["samples"] = []health.Sample{s}
	if w := callAPI(h.healthBatch, req); w.Code != http.StatusForbidden {
		t.Fatalf("disabled device accepted: %d %s", w.Code, w.Body)
	}
	mustExec(t, pool, "UPDATE device SET status=1 WHERE device_id=1")
	write()
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	mustExec(t, pool, "UPDATE device SET old_id=1 WHERE device_id=1")
	if w := callAPI(h.healthBatch, req); w.Code != http.StatusConflict {
		t.Fatalf("old epoch accepted: %d %s", w.Code, w.Body)
	}
	req["bindingVersion"] = 3
	write()
	var n int
	if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM health_change").Scan(&n); err != nil || n != 4 {
		t.Fatalf("durable changes=%d err=%v", n, err)
	}
}
