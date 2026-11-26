package handlers

import (
	"context"
	"fmt"
	"testing"

	"yihe-care/internal/health"
)

func TestHealthDailyBatchOrderAndExistingResultsMySQL(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	h := testHandler(t, pool)
	admin := capabilityAdmin(h, h.healthDaily)
	req := map[string]any{
		"oldId": 1, "source": "watch", "sourceDevice": "WATCH001",
		"metric": "heart_rate", "semantics": "instant",
		"day": "2026-10-01", "offsetMinutes": 480, "days": 31,
	}
	first := api(t, admin, req)["items"].([]any)
	if len(first) != 31 {
		t.Fatalf("got %d days", len(first))
	}
	for n, raw := range first {
		row := raw.(map[string]any)
		if row["day"] != fmt.Sprintf("2026-10-%02d", n+1) ||
			row["status"] != "pending" || row["statistics"] != nil {
			t.Fatalf("day %d: %#v", n, row)
		}
	}
	key := health.DailyKey{
		OldID: 1, Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate",
		Semantics: "instant", Day: "2026-10-01", OffsetMinutes: 480,
	}
	if _, err := pool.ExecContext(context.Background(),
		"UPDATE metric_daily SET dirty=false,computed_revision=5,result=? WHERE cache_key=?",
		`{"count":7,"mean":72}`, key.ID()); err != nil {
		t.Fatal(err)
	}
	key.Day = "2026-10-03"
	if _, err := pool.ExecContext(context.Background(),
		"UPDATE metric_daily SET dirty=false,error_text='source conflict' WHERE cache_key=?", key.ID()); err != nil {
		t.Fatal(err)
	}
	again := api(t, admin, req)["items"].([]any)
	if len(again) != 31 ||
		again[0].(map[string]any)["status"] != "ready" ||
		again[0].(map[string]any)["computedRevision"] != float64(5) ||
		again[0].(map[string]any)["statistics"].(map[string]any)["count"] != float64(7) ||
		again[1].(map[string]any)["status"] != "pending" ||
		again[2].(map[string]any)["status"] != "conflict" ||
		again[2].(map[string]any)["message"] != "source conflict" {
		t.Fatalf("existing daily results changed: %#v", again[:3])
	}
	var count int
	if err := pool.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM metric_daily WHERE old_id=1").Scan(&count); err != nil || count != 31 {
		t.Fatalf("batch should be idempotent: rows=%d err=%v", count, err)
	}
	req["days"] = 1
	if items := api(t, admin, req)["items"].([]any); len(items) != 1 ||
		items[0].(map[string]any)["statistics"].(map[string]any)["mean"] != float64(72) {
		t.Fatalf("one-day query changed: %#v", items)
	}
}
