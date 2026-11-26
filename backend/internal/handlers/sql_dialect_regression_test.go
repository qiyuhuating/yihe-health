package handlers

import (
	"context"
	"strings"
	"testing"
)

func TestDailyUpsertUsesNativeMySQL(t *testing.T) {
	if !strings.Contains(metricDailyEnsureSQL, "ON DUPLICATE KEY UPDATE") || strings.Contains(metricDailyEnsureSQL, "ON CONFLICT") {
		t.Fatalf("daily SQL is not native MySQL: %s", metricDailyEnsureSQL)
	}
}

func TestDeviceReportDailyUpsertPreservesMissingFields(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)

	api(t, h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"step": 100, "sleep": 8, "sleepState": "1",
		"beTime": "2026-10-28 08:00:00",
	})
	api(t, h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"step":   250,
		"beTime": "2026-10-28 20:00:00",
	})

	var rows, step, sleep int
	var state string
	err := pool.QueryRowContext(context.Background(),
		"SELECT count(*),max(step),max(sleep),max(sleep_sate) FROM watch_day_info WHERE device_id=1").
		Scan(&rows, &step, &sleep, &state)
	if err != nil {
		t.Fatal(err)
	}
	if rows != 1 || step != 250 || sleep != 8 || state != "1" {
		t.Fatalf("daily upsert lost prior fields: rows=%d step=%d sleep=%d state=%q", rows, step, sleep, state)
	}
}
