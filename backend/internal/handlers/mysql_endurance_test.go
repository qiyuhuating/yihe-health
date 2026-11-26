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
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/rules"
)

// Opt-in: runs ONLY with SOAK_MYSQL=1 against a disposable TEST_MYSQL_DSN.
func TestMySQLTwentyMinuteSoak(t *testing.T) {
	if os.Getenv("SOAK_MYSQL") != "1" {
		t.Skip("opt-in: set SOAK_MYSQL=1")
	}
	if os.Getenv("TEST_MYSQL_DSN") == "" {
		t.Fatal("disposable TEST_MYSQL_DSN required")
	}
	duration := 20 * time.Minute
	if v := os.Getenv("SOAK_DURATION"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute || d > 25*time.Minute {
			t.Fatalf("invalid SOAK_DURATION %q", v)
		}
		duration = d
	}
	const workers, batch = 4, 200
	pool := testDatabase(t)
	seedDevice(t, pool)
	handler := testHandler(t, pool)
	server := httptest.NewServer(http.HandlerFunc(handler.healthBatch))
	defer server.Close()
	client := &http.Client{Timeout: 35 * time.Second, Transport: &http.Transport{MaxIdleConns: 8, MaxIdleConnsPerHost: 8}}
	defer client.CloseIdleConnections()
	started := time.Now()
	deadline := started.Add(duration)
	runID := strconv.FormatInt(started.UnixNano(), 36)
	var mu sync.Mutex
	var count, ack, requestErrors, ruleErrors, seq int64
	statuses := map[string]int64{}
	latencies := make([]float64, 0, 8000)
	minutes := make([]map[string]any, 0, 26)
	stop := make(chan struct{})
	var bg sync.WaitGroup
	bg.Add(1)
	go func() {
		defer bg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		engine := &rules.Engine{DB: pool, Box: handler.Box, Workers: 2}
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
				err := engine.Tick(ctx)
				cancel()
				if err != nil {
					mu.Lock()
					ruleErrors++
					mu.Unlock()
				}
			}
		}
	}()
	bg.Add(1)
	go func() {
		defer bg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				mu.Lock()
				point := map[string]any{"elapsed_seconds": time.Since(started).Seconds(), "requests": count, "acknowledged_requests": ack, "request_errors": requestErrors, "rule_errors": ruleErrors, "http_500": statuses["500"], "http_503": statuses["503"]}
				mu.Unlock()
				stat := pool.DB.Stats()
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				point["pool_in_use"] = stat.InUse
				point["pool_wait_count"] = stat.WaitCount
				point["pool_wait_ms"] = stat.WaitDuration.Seconds() * 1000
				point["heap_alloc_bytes"] = mem.HeapAlloc
				point["gc_cycles"] = mem.NumGC
				mu.Lock()
				minutes = append(minutes, point)
				mu.Unlock()
				t.Logf("MySQL endurance minute: %+v", point)
			}
		}
	}()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				mu.Lock()
				seq++
				id := seq
				mu.Unlock()
				now := time.Now().UTC().Format(time.RFC3339Nano)
				samples := make([]map[string]any, 0, batch)
				for i := 0; i < batch; i++ {
					samples = append(samples, map[string]any{"recordId": fmt.Sprintf("soak-%s-%d-%d", runID, id, i), "source": "ci-synthetic", "sourceDevice": "WATCH001", "metric": "heart_rate", "unit": "count/min", "value": 72, "start": now, "end": now, "version": 1})
				}
				body, _ := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": samples})
				at := time.Now()
				status := "transport_error"
				ok := false
				req, err := http.NewRequest(http.MethodPost, server.URL, bytes.NewReader(body))
				if err == nil {
					req.Header.Set("Content-Type", "application/json")
					resp, e := client.Do(req)
					if e == nil {
						status = strconv.Itoa(resp.StatusCode)
						data, e := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
						resp.Body.Close()
						if e == nil && resp.StatusCode == http.StatusOK {
							var out map[string]any
							if json.Unmarshal(data, &out) == nil {
								ok = out["returnCode"] == "1111" && out["accepted"] == float64(batch) && out["duplicates"] == float64(0)
							}
						}
					}
				}
				ms := float64(time.Since(at)) / float64(time.Millisecond)
				mu.Lock()
				count++
				statuses[status]++
				latencies = append(latencies, ms)
				if ok {
					ack++
				} else {
					requestErrors++
				}
				mu.Unlock()
				if left := time.Until(deadline); left > 2*time.Second {
					time.Sleep(2 * time.Second)
				} else if left > 0 {
					time.Sleep(left)
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	bg.Wait()
	elapsed := time.Since(started).Seconds()
	mu.Lock()
	record := map[string]any{"kind": "synthetic-httptest-real-mysql-soak", "run_id": runID, "go_version": runtime.Version(), "requested_seconds": duration.Seconds(), "elapsed_seconds": elapsed, "batch_size": batch, "workers": workers, "requests": count, "acknowledged_requests": ack, "acknowledged_samples": ack * batch, "request_errors": requestErrors, "rule_errors": ruleErrors, "http_statuses": statuses, "per_minute": minutes, "limitations": []string{"real isolated MySQL but single httptest HTTP handler", "not real devices or external notification delivery", "no automatic retry of uncertain writes"}}
	ordered := append([]float64(nil), latencies...)
	mu.Unlock()
	sort.Float64s(ordered)
	if len(ordered) > 0 {
		percentile := func(p int) float64 {
			i := (len(ordered)*p+99)/100 - 1
			if i < 0 {
				i = 0
			}
			if i >= len(ordered) {
				i = len(ordered) - 1
			}
			return ordered[i]
		}
		record["latency_ms"] = map[string]float64{"p50": percentile(50), "p95": percentile(95), "p99": percentile(99), "max": ordered[len(ordered)-1]}
	}
	if elapsed > 0 {
		record["acknowledged_samples_per_second"] = float64(ack*batch) / elapsed
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var persisted, changes int64
	errSample := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample").Scan(&persisted)
	errChange := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_change").Scan(&changes)
	record["persisted_samples"] = persisted
	record["persisted_changes"] = changes
	if errSample != nil {
		record["sample_count_error"] = errSample.Error()
	}
	if errChange != nil {
		record["change_count_error"] = errChange.Error()
	}
	passed := errSample == nil && errChange == nil && requestErrors == 0 && ruleErrors == 0 && count > 0 && persisted == ack*batch && changes == persisted
	record["passed"] = passed
	data, e := json.MarshalIndent(record, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	target := os.Getenv("SOAK_OUTPUT_PATH")
	if target == "" {
		target = filepath.Join(t.TempDir(), "mysql-soak.json")
	}
	if e := os.MkdirAll(filepath.Dir(target), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(target, append(data, '\n'), 0644); e != nil {
		t.Fatal(e)
	}
	t.Logf("soak summary saved %s, passed=%t", target, passed)
	if !passed {
		t.Fatalf("MySQL endurance failed: HTTP errors=%d rule errors=%d acknowledged samples=%d persisted=%d changes=%d", requestErrors, ruleErrors, ack*batch, persisted, changes)
	}
}
