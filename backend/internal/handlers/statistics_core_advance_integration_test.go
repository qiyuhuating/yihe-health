package handlers

import (
	"container/list"
	"hash/maphash"
	"testing"
	"time"

	"yihe-care/internal/health"
)

// Exercise the Go projection pipeline even without the optional native engine.
func TestDeferredCoreAdvancePreservesCommitOrder(t *testing.T) {
	t.Setenv("HEALTH_LATE_DEVICE_LOCK", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "projection", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	req := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}}
	write := func() {
		t.Helper()
		if out := api(t, h.healthBatch, req); out["returnCode"] != "1111" {
			t.Fatal(out)
		}
	}
	write()
	var id string
	var revision int64
	if err := pool.QueryRowContext(t.Context(), "SELECT LOWER(HEX(sample_id)),revision FROM health_sample WHERE old_id=1").Scan(&id, &revision); err != nil {
		t.Fatal(err)
	}
	cache := &coreCache{seed: maphash.MakeSeed(), perShard: 1 << 20}
	for i := range cache.shards {
		cache.shards[i].entries = make(map[coreKey]*list.Element)
	}
	h.healthCache = &statisticsCache{core: cache}
	key := coreKey{1, "watch", "WATCH001"}
	s.Semantics = "instant"
	cache.put(makeCoreSeries(key, revision, at.Add(-time.Second).UnixNano(), at.Add(time.Second).UnixNano(), []health.Sample{s}, []string{id}, 1, time.Now()))
	gate := make(chan struct{})
	defer close(gate)
	started := make(chan struct{})
	if !cache.advance.submit(func() { close(started); <-gate }) {
		t.Fatal("could not block worker")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	s.Version, s.Value = 2, health.Number(80)
	req["samples"] = []health.Sample{s}
	write()
	s.Version, s.Value = 3, health.Number(92)
	req["samples"] = []health.Sample{s}
	write()
	latest, err := health.StatisticsRevision(t.Context(), pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cache.atRevision(key, latest, time.Now()) != nil {
		t.Fatal("projection advanced while worker blocked")
	}
	gate <- struct{}{}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := cache.atRevision(key, latest, time.Now()); got != nil {
			if len(got.values) != 1 || got.values[0] != 92 {
				t.Fatal("incorrect projection", got.values)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("committed projection chain not advanced")
		}
		time.Sleep(time.Millisecond)
	}
}
