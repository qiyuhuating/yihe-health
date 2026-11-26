//go:build yihecore && cgo && !purego && ((windows && amd64) || (linux && (amd64 || arm64)))

package handlers

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/config"
	"yihe-care/internal/health"
	"yihe-care/internal/observability"
	"yihe-care/internal/session"
)

func coreSamples(at time.Time, n int) []health.Sample {
	samples := make([]health.Sample, n)
	for i := range samples {
		start := at.Add(time.Duration(i) * time.Minute)
		samples[i] = health.Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Semantics: "instant", Unit: "count/min", Value: health.Number(float64(60 + i%31)), Start: start, End: start, Version: 1}
	}
	return samples
}

func TestCoreColumnCacheParity(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	samples := coreSamples(at, 256)
	key := coreKey{1, "watch", "WATCH001"}
	c := newCoreCache(4 << 20)
	series := makeCoreSeries(key, 7, at.UnixNano(), at.Add(256*time.Minute).UnixNano(), samples, nil, len(samples), at)
	if series == nil {
		t.Fatal("admission rejected valid samples")
	}
	c.put(series)
	for i := range 257 {
		from, to := at.Add(time.Duration(i)*time.Minute), at.Add(257*time.Minute)
		if i == 256 {
			from, to = at.Add(255*time.Minute+time.Second), at.Add(256*time.Minute)
		} else {
			to = at.Add(time.Duration(min(256, i+17)) * time.Minute)
		}
		var subset []health.Sample
		for _, sample := range samples {
			if sample.InWindow(from, to) {
				subset = append(subset, sample)
			}
		}
		want, err := health.Aggregate(subset, from, to)
		got, ok := c.get(key, 7, from.UnixNano(), to.UnixNano(), at)
		if err != nil || !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("window %d: got %+v want %+v hit=%v err=%v", i, got, want, ok, err)
		}
	}
	// Exercise the vectorized path and the ordered fractional fallback.
	for _, fractional := range []bool{false, true} {
		x := coreSamples(at, 256)
		if fractional {
			x[17].Value = health.Number(71.125)
		}
		s := makeCoreSeries(key, 7, at.UnixNano(), at.Add(256*time.Minute).UnixNano(), x, nil, len(x), at)
		if s == nil || s.exactInteger == fractional {
			t.Fatal("incorrect SIMD admission")
		}
		want, err := health.Aggregate(x, at, at.Add(256*time.Minute))
		got, ok := coreQuery(s, s.from, s.to)
		if err != nil || !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("fractional=%v native=%+v go=%+v", fractional, got, want)
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				got, ok := c.get(key, 7, at.UnixNano(), at.Add(time.Minute).UnixNano(), at)
				if !ok || got.Count != 1 || *got.Mean != 60 {
					t.Error("concurrent native query mismatch")
					return
				}
			}
		}()
	}
	wg.Wait()
	if _, ok := c.get(key, 8, series.from, series.to, at); ok {
		t.Fatal("stale revision served")
	}
}

func TestCoreStatisticsMySQL(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "12")
	pool := testDatabase(t)
	if pool.StatisticsPool() == pool.DB || pool.StatisticsPool().Stats().MaxOpenConnections != 12 {
		t.Fatal("statistics pool isolation not enabled")
	}
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 8 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	plain := testHandler(t, pool)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	samples := coreSamples(at, 256)
	batch := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1}
	for start := 0; start < len(samples); start += 64 {
		batch["samples"] = samples[start : start+64]
		api(t, plain.healthBatch, batch)
	}
	query := healthStatisticsRequest{OldID: 1, Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Semantics: "instant", From: at, To: at.Add(256 * time.Minute)}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	uncached := withHealthPrincipal(plain, session.Principal{Role: auth.RoleAdmin, ID: 11}, plain.healthStatistics)
	api(t, admin, query)
	observability.Default.Set("yihe_health_core_hits_total", 0)
	query.From, query.To = at.Add(time.Minute), at.Add(131*time.Minute)
	check := func() {
		t.Helper()
		a, b := callAPI(admin, query), callAPI(uncached, query)
		if a.Code != 200 || b.Code != 200 || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
			t.Fatalf("native=%d %s sql=%d %s", a.Code, a.Body, b.Code, b.Body)
		}
	}
	check()
	metrics := httptest.NewRecorder()
	observability.Default.ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), "yihe_health_core_hits_total 1\n") {
		t.Fatal("subwindow did not use native projection")
	}
	// Distinct, covered windows: exact-result cache cannot explain the hits.
	var nativeTime, sqlTime time.Duration
	for i := 2; i < 22; i++ {
		query.From, query.To = at.Add(time.Duration(i)*time.Minute), at.Add(time.Duration(i+130)*time.Minute)
		start := time.Now()
		a := callAPI(admin, query)
		nativeTime += time.Since(start)
		start = time.Now()
		b := callAPI(uncached, query)
		sqlTime += time.Since(start)
		if a.Code != 200 || b.Code != 200 || !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
			t.Fatalf("window %d: native=%d %s sql=%d %s", i, a.Code, a.Body, b.Code, b.Body)
		}
	}
	t.Logf("20 distinct warm windows: native mean=%s SQL mean=%s", nativeTime/20, sqlTime/20)
	query.From, query.To = at.Add(time.Minute), at.Add(131*time.Minute)
	observability.Default.Set("yihe_health_core_incremental_hits_total", 0)
	samples[1].Version, samples[1].Value = 2, health.Number(120)
	batch["samples"] = []health.Sample{samples[1]}
	api(t, plain.healthBatch, batch)
	check()
	samples[1].Version, samples[1].Deleted = 3, true
	batch["samples"] = []health.Sample{samples[1]}
	api(t, plain.healthBatch, batch)
	check()
	// A moved point crosses the existing projection's coverage.
	samples[2].Version = 2
	samples[2].Start = at.Add(300 * time.Minute)
	samples[2].End = samples[2].Start
	batch["samples"] = []health.Sample{samples[2]}
	api(t, plain.healthBatch, batch)
	check()
	metrics = httptest.NewRecorder()
	observability.Default.ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), "yihe_health_core_incremental_hits_total 3\n") {
		t.Fatalf("correction, deletion and move did not all use incremental projection")
	}
	// A local commit advances a matching immutable projection without querying
	// the change table; a foreign commit creates a gap that must be read from SQL.
	key := coreKey{1, "watch", "WATCH001"}
	samples[3].Version, samples[3].Value = 2, health.Number(98)
	batch["samples"] = []health.Sample{samples[3]}
	api(t, h.healthBatch, batch)
	revision, err := health.StatisticsRevision(t.Context(), pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.healthCache.core.get(key, revision, query.From.UnixNano(), query.To.UnixNano(), time.Now()); !ok {
		t.Fatal("committed local correction did not advance the projection")
	}
	check()
	samples[4].Version, samples[4].Value = 2, health.Number(103)
	batch["samples"] = []health.Sample{samples[4]}
	api(t, plain.healthBatch, batch)
	samples[5].Version, samples[5].Value = 2, health.Number(106)
	batch["samples"] = []health.Sample{samples[5]}
	api(t, h.healthBatch, batch)
	revision, err = health.StatisticsRevision(t.Context(), pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h.healthCache.core.get(key, revision, query.From.UnixNano(), query.To.UnixNano(), time.Now()); ok {
		t.Fatal("local commit skipped a foreign committed correction")
	}
	check()
	pool.Close()
	if got := callAPI(admin, query); got.Code != http.StatusInternalServerError {
		t.Fatalf("database outage returned %d", got.Code)
	}
}

func TestCoreRebuildAfterHistoricalCorrection(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	samples := coreSamples(at, 3)
	samples[0], samples[2] = samples[2], samples[0]
	ids := []string{"third", "second", "first"}
	to := at.Add(3 * time.Minute)
	series := makeCoreSeries(coreKey{1, "watch", "WATCH001"}, 7, at.UnixNano(), to.UnixNano(), samples, ids, len(samples), at)
	if series == nil || !reflect.DeepEqual(series.ids, []string{"first", "second", "third"}) {
		t.Fatalf("unordered integer projection not admitted or IDs misplaced: %+v", series)
	}
	got, ok := coreQuery(series, at.UnixNano(), at.Add(time.Minute).UnixNano())
	if !ok || got.Count != 1 || *got.Mean != 60 {
		t.Fatalf("reordered projection mismatch: %+v", got)
	}
	samples[0].Value = health.Number(71.125)
	if series := makeCoreSeries(coreKey{1, "watch", "WATCH001"}, 8, at.UnixNano(), to.UnixNano(), samples, nil, len(samples), at); series != nil {
		t.Fatal("unordered fractional projection must preserve scalar order via fallback")
	}
}
