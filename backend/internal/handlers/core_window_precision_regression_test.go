package handlers

import (
	"context"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

func TestContinuousStatisticsSubmicrosecondWindow(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	h.healthCache = newStatisticsCache(1 << 20)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	samples := []health.Sample{}
	for i, ns := range []int{100, 500, 900} {
		point := at.Add(time.Duration(ns))
		samples = append(samples, health.Sample{RecordID: string(rune('a' + i)), Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(float64((i + 1) * 30)), Start: point, End: point, Version: 1})
	}
	api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": samples})
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	for _, tc := range []struct {
		end         time.Time
		count, mean float64
	}{{at.Add(900 * time.Nanosecond), 1, 60}, {at.Add(time.Second), 2, 75}} {
		query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": at.Add(500 * time.Nanosecond), "to": tc.end}
		for range 2 {
			out := api(t, admin, query)["statistics"].(map[string]any)
			if out["count"] != tc.count || out["mean"] != tc.mean {
				t.Errorf("window ending %s returned %v want count=%v mean=%v", tc.end.Format(time.RFC3339Nano), out, tc.count, tc.mean)
			}
		}
	}
}

func TestContinuousDailySubmicrosecondInterval(t *testing.T) {
	pool := dbtest.MySQL(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	samples := []health.Sample{
		{RecordID: "cross-midnight", Source: "watch", SourceDevice: "WATCH001", Metric: "steps", Unit: "count", Value: health.Number(10), Start: from.Add(-100 * time.Nanosecond), End: from.Add(900 * time.Nanosecond), Semantics: "interval", Version: 1},
		{RecordID: "ends-at-midnight", Source: "watch", SourceDevice: "WATCH001", Metric: "steps", Unit: "count", Value: health.Number(20), Start: from.Add(-2 * time.Microsecond), End: from, Semantics: "interval", Version: 1},
	}
	api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": samples})
	key := health.DailyKey{OldID: 1, Source: "watch", SourceDevice: "WATCH001", Metric: "steps", Semantics: "interval", Day: "2026-10-20"}
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "steps", "semantics": "interval", "day": key.Day, "offsetMinutes": 0, "days": 1}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthDaily)
	api(t, admin, query)
	if err := health.RefreshDaily(context.Background(), pool, key.ID(), h.Box); err != nil {
		t.Fatal(err)
	}
	item := api(t, admin, query)["items"].([]any)[0].(map[string]any)
	out, ok := item["statistics"].(map[string]any)
	if item["status"] != "ready" || !ok || out["count"] != float64(1) || out["total"] != float64(9) {
		t.Fatalf("cross-midnight fractional interval omitted or out-of-window overlap retained: %v", item)
	}
}
