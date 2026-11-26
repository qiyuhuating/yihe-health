package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

// This fixture must also compile unchanged at 60be581. It measures the real
// SaveBatch+commit path, not a copy of the implementation. dbtest always creates
// and drops an isolated database; never substitute a production connection.
func TestBatchHardwareAB(t *testing.T) {
	if os.Getenv("RUN_BATCH_HARDWARE_AB") != "1" {
		t.Skip("opt-in hardware A/B")
	}
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'perf-ab')"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	samples := make([]Sample, 200)
	for i := range samples {
		samples[i] = Sample{RecordID: fmt.Sprint(i), Source: "synthetic", SourceDevice: "synthetic", Metric: "heart_rate", Unit: "count/min", Value: Number(72), Start: at, End: at, Version: 1}
	}
	save := func(version int64) [3]time.Duration {
		for i := range samples {
			samples[i].Version = version
		}
		started := time.Now()
		tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		begun := time.Now()
		_, duplicates, err := SaveBatch(ctx, tx, Owner{OldID: 1}, samples)
		if err != nil || duplicates != 0 {
			tx.Rollback()
			t.Fatalf("save: duplicates=%d err=%v", duplicates, err)
		}
		written := time.Now()
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return [3]time.Duration{begun.Sub(started), written.Sub(begun), time.Since(written)}
	}
	const warmup, batches = 3, 500
	for version := int64(1); version <= warmup; version++ {
		save(version)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	durations := make([]time.Duration, 0, batches)
	phases := make([][3]time.Duration, 0, batches)
	start := time.Now()
	for batch := range batches {
		at := time.Now()
		phases = append(phases, save(int64(warmup+batch+1)))
		durations = append(durations, time.Since(at))
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	var sampleCount, eventCount int64
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM health_sample").Scan(&sampleCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM health_change").Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if sampleCount != 200 || eventCount != (warmup+batches)*200 {
		t.Fatalf("persisted counts: samples=%d events=%d", sampleCount, eventCount)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	percentile := func(percent int) float64 {
		return float64(durations[(len(durations)*percent+99)/100-1]) / float64(time.Millisecond)
	}
	result := map[string]any{
		"variant": os.Getenv("PERF_AB_VARIANT"), "go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0),
		"batches": batches, "samples_per_batch": 200, "errors": 0, "elapsed_ms": float64(elapsed) / float64(time.Millisecond),
		"samples_per_second": float64(batches*200) / elapsed.Seconds(),
		"p50_ms":             percentile(50), "p95_ms": percentile(95), "p99_ms": percentile(99),
		"allocated_bytes_per_batch": (after.TotalAlloc - before.TotalAlloc) / batches, "allocations_per_batch": (after.Mallocs - before.Mallocs) / batches,
		"gc_cycles": after.NumGC - before.NumGC, "gc_pause_ns": after.PauseTotalNs - before.PauseTotalNs,
		"persisted_samples": sampleCount, "persisted_events": eventCount,
	}
	phaseResults := make(map[string]any)
	for phase, name := range []string{"begin", "save", "commit"} {
		values := make([]time.Duration, len(phases))
		for i, p := range phases {
			values[i] = p[phase]
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		phaseResults[name] = map[string]float64{"p50_ms": float64(values[len(values)/2]) / float64(time.Millisecond), "p99_ms": float64(values[(len(values)*99+99)/100-1]) / float64(time.Millisecond)}
	}
	sort.Slice(phases, func(i, j int) bool {
		return phases[i][0]+phases[i][1]+phases[i][2] > phases[j][0]+phases[j][1]+phases[j][2]
	})
	tail := make([]map[string]float64, 0, 5)
	for _, p := range phases[:5] {
		tail = append(tail, map[string]float64{"begin_ms": float64(p[0]) / float64(time.Millisecond), "save_ms": float64(p[1]) / float64(time.Millisecond), "commit_ms": float64(p[2]) / float64(time.Millisecond)})
	}
	result["phases"] = phaseResults
	result["slowest_transactions"] = tail
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("HARDWARE_AB %s", data)
}
