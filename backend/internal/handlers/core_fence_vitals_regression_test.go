package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/vitalrepo"
)

func TestContinuousFencePointPresence(t *testing.T) {
	pool := dbtest.MySQL(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	for _, point := range []any{"absent", nil, []any{nil, 30}} {
		body := commandCredentials()
		body["coordinateSystem"], body["accuracy"], body["occurredAt"] = "WGS84", 5, time.Now().UTC()
		if point != "absent" {
			body["point"] = point
		}
		if w := callAPI(h.fencePoint, body); w.Code != http.StatusBadRequest {
			t.Errorf("missing/null point=%v accepted with HTTP %d", point, w.Code)
		}
	}
	body := commandCredentials()
	body["coordinateSystem"], body["accuracy"], body["occurredAt"], body["point"] = "WGS84", 5, time.Now().UTC(), []int{0, 0}
	if w := callAPI(h.fencePoint, body); w.Code != http.StatusOK {
		t.Fatalf("explicit zero coordinate rejected: HTTP %d %s", w.Code, w.Body.String())
	}
}

func TestContinuousZeroVitalsAreMissing(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	for i, tc := range []struct {
		hr, bp               int
		hrMissing, bpMissing bool
	}{{0, 120, true, false}, {72, 0, false, true}} {
		out := api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "hr": tc.hr, "bp": tc.bp})
		var stored vitalrepo.StoredMeasurements
		var owner, device int64
		var rate, pre *string
		if err := pool.QueryRowContext(context.Background(), "SELECT old_id,device_id,hr,bp,measurement_cipher,rate_state,pre_state FROM ratepre WHERE id=?", i+1).
			Scan(&owner, &device, &stored.HeartRate, &stored.Pressure, &stored.Cipher, &rate, &pre); err != nil {
			t.Fatal(err)
		}
		values, err := stored.Open(h.Box, int64(i+1), &owner, &device)
		if err != nil || stored.Cipher == nil || stored.HeartRate != nil || stored.Pressure != nil {
			t.Fatalf("vital signs were not protected: %v", err)
		}
		if (values.HeartRate == nil) != tc.hrMissing || (values.Pressure == nil) != tc.bpMissing ||
			(rate == nil) != tc.hrMissing || (pre == nil) != tc.bpMissing || out["rateState"] == "1" || out["preState"] == "1" {
			t.Errorf("zero sentinel became measurement/alarm: hr=%v bp=%v rate=%v pre=%v out=%v", values.HeartRate, values.Pressure, rate, pre, out)
		}
	}
	out := api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "hr": 0, "bp": 0})
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM ratepre").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 || out["rateState"] != nil || out["preState"] != nil {
		t.Fatalf("empty measurements created alarms: rows=%d out=%v", count, out)
	}
}
