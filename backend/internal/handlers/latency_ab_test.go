package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/health"
)

// Copy this fixture unchanged into 60be581 for the A/B baseline. This includes
// loopback HTTP, JSON decoding, real device authentication, row locks and commit.
// It excludes the outer production middleware, proxies and external device RTT.
func TestHTTPBatchLatencyAB(t *testing.T) {
	if os.Getenv("RUN_HTTP_LATENCY_AB") != "1" {
		t.Skip("opt-in real MySQL HTTP latency A/B")
	}
	option := func(key string, fallback, maximum int) int {
		raw := os.Getenv(key)
		if raw == "" {
			return fallback
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > maximum {
			t.Fatalf("invalid %s", key)
		}
		return v
	}
	size := option("BATCH_AB_SIZE", 200, 200)
	workers := option("BATCH_AB_WORKERS", 1, 8)
	const warmup = 3
	requests := option("BATCH_AB_REQUESTS", 500, 2000)
	metadataBytes := 0
	if raw := os.Getenv("BATCH_AB_METADATA_BYTES"); raw != "" && raw != "0" {
		metadataBytes = option("BATCH_AB_METADATA_BYTES", 1, 4096)
	}
	var metadata map[string]string
	if metadataBytes > 0 {
		metadata = map[string]string{"synthetic": strings.Repeat("x", metadataBytes)}
	}
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	server := httptest.NewServer(http.HandlerFunc(h.healthBatch))
	defer server.Close()
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConns: 16, MaxIdleConnsPerHost: 16}}
	defer client.CloseIdleConnections()
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	send := func(worker, version int) (time.Duration, error) {
		samples := make([]health.Sample, size)
		for i := range samples {
			samples[i] = health.Sample{RecordID: fmt.Sprintf("lat-%d-%d", worker, i), Source: "synthetic", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: int64(version), Metadata: metadata}
		}
		body, err := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": samples})
		if err != nil {
			return 0, err
		}
		req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			return time.Since(start), err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		elapsed := time.Since(start)
		if err != nil {
			return elapsed, err
		}
		var out struct {
			Code       string `json:"returnCode"`
			Accepted   int    `json:"accepted"`
			Duplicates int    `json:"duplicates"`
		}
		if json.Unmarshal(data, &out) != nil || resp.StatusCode != 200 || out.Code != "1111" || out.Accepted != size || out.Duplicates != 0 {
			return elapsed, fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
		}
		return elapsed, nil
	}
	var wg sync.WaitGroup
	failures := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for v := 1; v <= warmup; v++ {
				if _, err := send(w, v); err != nil {
					failures <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if len(failures) > 0 {
		t.Fatal(<-failures)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	durations := make([]time.Duration, requests)
	start := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := w; i < requests; i += workers {
				elapsed, err := send(w, warmup+i/workers+1)
				durations[i] = elapsed
				if err != nil {
					failures <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if len(failures) > 0 {
		t.Fatal(<-failures)
	}
	var samples, events int
	ctx := context.Background()
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM health_sample").Scan(&samples); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM health_change").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if samples != workers*size || events != (warmup*workers+requests)*size {
		t.Fatalf("persisted samples=%d events=%d", samples, events)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	percentile := func(p int) float64 { return float64(durations[(requests*p+99)/100-1]) / float64(time.Millisecond) }
	result := map[string]any{"variant": os.Getenv("PERF_AB_VARIANT"), "kind": "loopback-http-handler-real-mysql", "go": runtime.Version(), "gomaxprocs": runtime.GOMAXPROCS(0), "workers": workers, "metadata_bytes": metadataBytes, "requests": requests, "samples_per_request": size, "errors": 0, "elapsed_ms": float64(elapsed) / float64(time.Millisecond), "requests_per_second": float64(requests) / elapsed.Seconds(), "p50_ms": percentile(50), "p95_ms": percentile(95), "p99_ms": percentile(99), "max_ms": float64(durations[len(durations)-1]) / float64(time.Millisecond), "allocated_bytes_per_request": (after.TotalAlloc - before.TotalAlloc) / uint64(requests), "gc_cycles": after.NumGC - before.NumGC, "persisted_samples": samples, "persisted_events": events}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("HTTP_LATENCY_AB %s", data)
}
