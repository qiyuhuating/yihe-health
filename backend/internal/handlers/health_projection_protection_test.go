package handlers

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/health"
	"yihe-care/internal/rekey"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestHealthProjectionProtectionMySQL(t *testing.T) {
	ctx := context.Background()
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h.Box = nil
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	at := time.Now().UTC().Truncate(24 * time.Hour).Add(-15 * time.Hour)
	sample := health.Sample{RecordID: "health-legacy", Source: "watch", SourceDevice: "WATCH001",
		Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Value: health.Number(70), Start: at, End: at, Version: 1}
	other := sample
	other.Source = "watch-alt"
	upload := func(samples ...health.Sample) {
		t.Helper()
		api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": samples})
	}
	dailyKey := health.DailyKey{OldID: 1, Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Semantics: "instant", Day: at.Format(time.DateOnly)}
	dailyQuery := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate",
		"semantics": "instant", "day": dailyKey.Day, "offsetMinutes": 0, "days": 1}
	owner := map[string]any{"oldId": 1}
	dailyRead := withHealthPrincipal(h, admin, h.healthDaily)
	latestRead := withHealthPrincipal(h, admin, h.healthLatest)
	changeRead := withHealthPrincipal(h, admin, h.healthChanges)
	upload(sample, other)
	api(t, latestRead, owner)
	api(t, dailyRead, dailyQuery)
	if err := health.RefreshDaily(ctx, pool, dailyKey.ID(), nil); err != nil {
		t.Fatal(err)
	}

	old, err := securestore.NewKeyring("A", map[string]string{"A": residentTestKey("R")}, residentTestKey("C"))
	if err != nil {
		t.Fatal(err)
	}
	h.Box = old
	h.healthCache = nil
	sample.Value, sample.Version = health.Number(80), 2
	upload(sample)
	pending := func() []int {
		t.Helper()
		var counts [3]int
		if err := pool.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM health_change WHERE payload IS NOT NULL),
 (SELECT COUNT(*) FROM health_sample_latest WHERE payload IS NOT NULL),(SELECT COUNT(*) FROM metric_daily WHERE result IS NOT NULL)`).
			Scan(&counts[0], &counts[1], &counts[2]); err != nil {
			t.Fatal(err)
		}
		return counts[:]
	}
	if got := pending(); !reflect.DeepEqual(got, []int{2, 1, 1}) {
		t.Fatalf("configured writers left new projection plaintext: %v", got)
	}
	if report, err := health.BackfillProjections(ctx, pool.DB, old, 1, false); err != nil || len(report) != 3 || report[0].Before != 2 || report[0].Migrated != 0 {
		t.Fatalf("readonly scan: %v %v", report, err)
	}
	for round := 1; round <= 5; round++ {
		reports, err := health.BackfillProjections(ctx, pool.DB, old, 1, true)
		if (round < 4 && !errors.Is(err, health.ErrProjectionBackfillIncomplete)) || (round >= 4 && err != nil) {
			t.Fatalf("bounded backfill round %d: %v %v", round, reports, err)
		}
	}
	if got := pending(); !reflect.DeepEqual(got, []int{0, 0, 0}) {
		t.Fatalf("legacy projection plaintext remains: %v", got)
	}
	rawBackfill, err := health.BackfillHealthSamples(ctx, pool.DB, old, 1, true)
	if err != nil || len(rawBackfill) != 1 || rawBackfill[0].Before != 1 || rawBackfill[0].Migrated != 1 ||
		rawBackfill[0].Remaining == nil || *rawBackfill[0].Remaining != 0 {
		t.Fatalf("legacy sample backfill: %v %v", rawBackfill, err)
	}
	var rawSamples, indexedValues int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(payload),COUNT(rule_value) FROM health_sample").
		Scan(&rawSamples, &indexedValues); err != nil || rawSamples != 0 || indexedValues != 0 {
		t.Fatalf("sample or rule covering index still exposes values: %d/%d %v", rawSamples, indexedValues, err)
	}
	if err := health.RefreshDaily(ctx, pool, dailyKey.ID(), old); err != nil {
		t.Fatal(err)
	}
	beforeChanges := api(t, changeRead, owner)
	beforeLatest := api(t, latestRead, owner)
	beforeDaily := api(t, dailyRead, dailyQuery)
	statsRead := withHealthPrincipal(h, admin, h.healthStatistics)
	statsQuery := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate",
		"semantics": "instant", "from": at.Add(-time.Hour), "to": at.Add(time.Hour)}
	beforeStats := api(t, statsRead, statsQuery)
	baselineRead := withHealthPrincipal(h, admin, h.healthBaseline)
	beforeBaseline := api(t, baselineRead, owner)
	if len(beforeChanges["changes"].([]any)) != 3 || len(beforeLatest["items"].([]any)) != 2 ||
		beforeDaily["items"].([]any)[0].(map[string]any)["statistics"].(map[string]any)["mean"] != float64(80) ||
		beforeStats["statistics"].(map[string]any)["mean"] != float64(80) || beforeBaseline["value"] != float64(80) {
		t.Fatal("protected health reads lost original values")
	}

	current, _ := securestore.NewKeyring("B", map[string]string{"A": residentTestKey("R"), "B": residentTestKey("S")}, residentTestKey("C"))
	retired, _ := securestore.NewKeyring("B", map[string]string{"B": residentTestKey("S")}, residentTestKey("C"))
	if report, err := rekey.Run(ctx, pool.DB, current, old, rekey.Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 1000}); err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized {
		t.Fatalf("projection rekey failed: %v", err)
	}
	h.Box = retired
	if !reflect.DeepEqual(api(t, changeRead, owner), beforeChanges) ||
		!reflect.DeepEqual(api(t, latestRead, owner), beforeLatest) ||
		!reflect.DeepEqual(api(t, dailyRead, dailyQuery), beforeDaily) ||
		!reflect.DeepEqual(api(t, statsRead, statsQuery), beforeStats) ||
		!reflect.DeepEqual(api(t, baselineRead, owner), beforeBaseline) {
		t.Fatal("retired-reader configuration changed authorized projection responses")
	}
	h.Box = old
	for _, endpoint := range []struct {
		read  http.HandlerFunc
		query map[string]any
	}{{changeRead, owner}, {latestRead, owner}, {dailyRead, dailyQuery}, {statsRead, statsQuery}, {baselineRead, owner}} {
		if got := callAPI(endpoint.read, endpoint.query); got.Code != http.StatusInternalServerError {
			t.Fatal("old-only reader received protected payloads after rotation")
		}
	}
	sample.Version++
	badUpdate := callAPI(h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret,
		"bindingVersion": 1, "samples": []health.Sample{sample}})
	if badUpdate.Code != http.StatusInternalServerError {
		t.Fatal("old-only writer replaced a protected sample")
	}
	h.Box = retired
	var revision int64
	var foreignCipher string
	if err := pool.QueryRowContext(ctx, "SELECT MIN(revision),(SELECT payload_cipher FROM health_sample_latest LIMIT 1) FROM health_change").
		Scan(&revision, &foreignCipher); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, "UPDATE health_change SET payload_cipher=? WHERE revision=?", foreignCipher, revision)
	if got := callAPI(changeRead, owner); got.Code != http.StatusInternalServerError {
		t.Fatal("change feed accepted a ciphertext from a different storage scope")
	}
}
