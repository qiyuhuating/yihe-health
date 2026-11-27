package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/health"
	"yihe-care/internal/observability"
	"yihe-care/internal/session"
)

// TestMixedSOSLatencyAB is opt-in. It uses disposable MySQL, real HTTP and
// continuously loaded, independently owned devices; SOS shares two devices
// with uploads. Report rejected uploads separately from successful latencies.
func TestMixedSOSLatencyAB(t *testing.T) {
	if os.Getenv("RUN_MIXED_SOS_AB") != "1" {
		t.Skip("opt-in mixed SOS latency A/B")
	}
	workers := 6
	if raw := os.Getenv("MIXED_SOS_WORKERS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 2 || n > 8 {
			t.Fatal("invalid MIXED_SOS_WORKERS")
		}
		workers = n
	}
	const batchSize = 64
	const requests = 200
	authMix := os.Getenv("MIXED_SOS_AUTH") == "1"
	pool := testDatabase(t)
	seedDevice(t, pool)
	for i := 2; i <= workers; i++ {
		mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(?,?)", i, fmt.Sprintf("resident-%d", i))
		mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT ?,?,secret,? FROM device WHERE device_id=1", i, fmt.Sprintf("WATCH%03d", i), i)
	}
	h := testHandler(t, pool)
	if raw := os.Getenv("MIXED_HEALTH_LIMIT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > healthWriteConcurrency {
			t.Fatal("invalid MIXED_HEALTH_LIMIT")
		}
		h.healthWrites.limit = n
		h.healthWrites.urgentLimit = min(n, healthWriteUrgentConcurrency)
	}
	warmLatest := os.Getenv("MIXED_SOS_WARM_LATEST") == "1"
	if warmLatest {
		for i := 1; i <= workers; i++ {
			if _, err := health.Latest(t.Context(), pool, int64(i), h.Box); err != nil {
				t.Fatal(err)
			}
		}
	}
	mux := http.NewServeMux()
	var app http.Handler = mux
	var profile *session.Session
	const phone, password = "13800000041", "MixedFixturePassword123!"
	if authMix {
		seedCareAdminDuty(t, pool, 41, "supervisor")
		hash, err := auth.Hash(password)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, pool, "UPDATE users SET user_phone=?,password=? WHERE user_id=41", phone, hash)
		h.Sessions.UseDatabase(pool.DB, h.Box)
		profile = h.Sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: 41, Phone: phone})
		if profile == nil {
			t.Fatal("fixture profile")
		}
		h.Routes(mux)
		app = AuthenticationAdmission(csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, h.EnforceAdminDuty(h.EnforceDeviceSignatures(mux)))))
	} else {
		mux.HandleFunc("/batch", h.healthBatch)
		mux.HandleFunc("/sos", h.deviceSos)
	}
	var phaseMu sync.Mutex
	phases := make(map[string][]float64)
	if os.Getenv("MIXED_TRACE_PHASES") == "1" {
		next := app
		app = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lane := ""
			switch r.URL.Path {
			case "/batch", "/api/v1/device/healthBatch":
				lane = "health"
			case "/sos", "/api/v1/device/sos":
				lane = "sos"
			case "/api/v1/auth/login":
				lane = "login"
			}
			if lane != "" {
				ctx := observability.WithTransactionTrace(r.Context(), func(stage string, elapsed time.Duration) {
					phaseMu.Lock()
					key := lane + "." + stage
					phases[key] = append(phases[key], float64(elapsed)/float64(time.Millisecond))
					phaseMu.Unlock()
				})
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
	server := httptest.NewServer(app)
	defer server.Close()
	client := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{MaxIdleConns: 24, MaxIdleConnsPerHost: 24}}
	defer client.CloseIdleConnections()
	send := func(path string, payload any) (float64, int, error) {
		body, err := json.Marshal(payload)
		if err != nil {
			return 0, 0, err
		}
		start := time.Now()
		urlPath := path
		if authMix {
			if path == "/batch" {
				urlPath = "/api/v1/device/healthBatch"
			} else {
				urlPath = "/api/v1/device/sos"
			}
		}
		resp, err := client.Post(server.URL+urlPath, "application/json", bytes.NewReader(body))
		if err != nil {
			return 0, 0, err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		ms := float64(time.Since(start)) / float64(time.Millisecond)
		if err != nil {
			return ms, resp.StatusCode, err
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			return ms, resp.StatusCode, nil
		}
		var out map[string]any
		if err = json.Unmarshal(data, &out); err != nil || resp.StatusCode != 200 || out["returnCode"] != "1111" {
			return ms, resp.StatusCode, fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
		}
		if path == "/batch" && (out["accepted"] != float64(batchSize) || out["duplicates"] != float64(0)) {
			return ms, resp.StatusCode, fmt.Errorf("invalid batch ack: %s", data)
		}
		return ms, resp.StatusCode, nil
	}
	var mu sync.Mutex
	var healthMS, sosMS, profileMS, loginMS []float64
	var rejected, profileRejected, loginRejected int
	var firstErr error
	fail := func(e error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = e
		}
		mu.Unlock()
	}
	stop := make(chan struct{})
	var writers sync.WaitGroup
	at := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	var before, after mixedDBSnapshot
	if authMix {
		before = readMixedDBSnapshot(t, pool.DB)
	}
	start := time.Now()
	if authMix {
		// Fixed closed-loop workload: two continuous authenticated profile
		// readers and one repeated real password/CSRF/session lifecycle.
		// The login worker paces successful cycles to stay below the
		// anonymous creation token rate. No fabricated login results.
		for range 2 {
			writers.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					ms, _, _, status, err := mixedAuthRequest(client, server.URL, "GET", "/api/v1/auth/session", profile.ID, "", nil)
					if err != nil || (status != 200 && status != 429) {
						fail(fmt.Errorf("profile status=%d: %v", status, err))
						return
					}
					mu.Lock()
					if status == 429 {
						profileRejected++
					} else {
						profileMS = append(profileMS, ms)
					}
					mu.Unlock()
				}
			})
		}
		writers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, body, cookie, status, err := mixedAuthRequest(client, server.URL, "GET", "/api/v1/auth/session", "", "", nil)
				if err != nil {
					fail(err)
					return
				}
				if status == 429 {
					mu.Lock()
					loginRejected++
					mu.Unlock()
					time.Sleep(150 * time.Millisecond)
					continue
				}
				if status != 200 || cookie == "" {
					fail(fmt.Errorf("bootstrap status=%d", status))
					return
				}
				var bootstrap struct{ CSRFToken string }
				if err = json.Unmarshal(body, &bootstrap); err != nil || bootstrap.CSRFToken == "" {
					fail(fmt.Errorf("bootstrap token: %v", err))
					return
				}
				ms, body, logged, status, err := mixedAuthRequest(client, server.URL, "POST", "/api/v1/auth/login", cookie, bootstrap.CSRFToken, map[string]any{"phone": phone, "password": password})
				if err != nil {
					fail(err)
					return
				}
				if status == 429 {
					mu.Lock()
					loginRejected++
					mu.Unlock()
					time.Sleep(150 * time.Millisecond)
					continue
				}
				var result struct {
					Authenticated bool
					ReturnCode    string
				}
				if status != 200 || logged == "" || json.Unmarshal(body, &result) != nil || !result.Authenticated || result.ReturnCode != "1111" {
					fail(fmt.Errorf("login status=%d: %s", status, body))
					return
				}
				mu.Lock()
				loginMS = append(loginMS, ms)
				mu.Unlock()
				// Fetch the newly rotated token before actual logout.
				_, body, _, status, err = mixedAuthRequest(client, server.URL, "GET", "/api/v1/auth/session", logged, "", nil)
				if err != nil || status != 200 || json.Unmarshal(body, &bootstrap) != nil || bootstrap.CSRFToken == "" {
					fail(fmt.Errorf("login profile status=%d: %v", status, err))
					return
				}
				_, _, _, status, err = mixedAuthRequest(client, server.URL, "POST", "/api/v1/auth/logout", logged, bootstrap.CSRFToken, map[string]any{})
				if err != nil || status != 200 {
					fail(fmt.Errorf("logout status=%d: %v", status, err))
					return
				}
				time.Sleep(150 * time.Millisecond)
			}
		})
	}
	for w := 1; w <= workers; w++ {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			device := fmt.Sprintf("WATCH%03d", w)
			for v := 1; ; {
				select {
				case <-stop:
					return
				default:
				}
				samples := make([]health.Sample, batchSize)
				for i := range samples {
					samples[i] = health.Sample{RecordID: fmt.Sprintf("mixed-%d-%d", w, i), Source: "synthetic", SourceDevice: device, Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: int64(v)}
				}
				ms, status, err := send("/batch", map[string]any{"deviceNo": device, "secret": testSecret, "bindingVersion": 1, "samples": samples})
				if err != nil {
					fail(err)
					return
				}
				mu.Lock()
				if status == 503 {
					rejected++
				} else {
					healthMS = append(healthMS, ms)
					v++
				}
				mu.Unlock()
				if status == 503 {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(w)
	}
	time.Sleep(250 * time.Millisecond)
	var emergency sync.WaitGroup
	for w := 1; w <= 2; w++ {
		emergency.Add(1)
		go func(w int) {
			defer emergency.Done()
			for i := 0; i < requests/2; i++ {
				ms, status, err := send("/sos", map[string]any{"deviceNo": fmt.Sprintf("WATCH%03d", w), "secret": testSecret, "bindingVersion": 1, "requestId": fmt.Sprintf("mixed-sos-%d-%d", w, i)})
				if err != nil || status != 200 {
					fail(fmt.Errorf("SOS status=%d: %v", status, err))
					return
				}
				mu.Lock()
				sosMS = append(sosMS, ms)
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
			}
		}(w)
	}
	emergency.Wait()
	close(stop)
	writers.Wait()
	elapsed := time.Since(start).Seconds()
	if authMix {
		after = readMixedDBSnapshot(t, pool.DB)
		if after.commits <= before.commits || len(after.buckets) == 0 {
			t.Fatal("missing Performance Schema COMMIT samples; use an isolated server with digest capacity")
		}
	}
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	if len(sosMS) != requests || len(healthMS) == 0 {
		t.Fatalf("missing work: SOS=%d health=%d", len(sosMS), len(healthMS))
	}
	var incidents, changes int
	if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM care_incident").Scan(&incidents); err != nil || incidents != requests {
		t.Fatalf("durable incidents=%d err=%v", incidents, err)
	}
	if err := pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM health_change").Scan(&changes); err != nil || changes != len(healthMS)*batchSize {
		t.Fatalf("durable changes=%d ack=%d err=%v", changes, len(healthMS)*batchSize, err)
	}
	stats := func(v []float64) map[string]float64 {
		sort.Float64s(v)
		p := func(n int) float64 { return v[(len(v)*n+99)/100-1] }
		return map[string]float64{"p50_ms": p(50), "p95_ms": p(95), "p99_ms": p(99), "max_ms": v[len(v)-1]}
	}
	if len(phases) > 0 {
		stageStats := make(map[string]any, len(phases))
		for k, v := range phases {
			stageStats[k] = map[string]any{"count": len(v), "latency": stats(v)}
		}
		phaseJSON, err := json.Marshal(stageStats)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MIXED_PHASES %s", phaseJSON)
	}
	if authMix {
		if len(profileMS) == 0 || len(loginMS) == 0 {
			t.Fatal("mixed authentication workload did not complete")
		}
		authOut, _ := json.Marshal(map[string]any{"profiles": len(profileMS), "profile": stats(profileMS), "profile_rejected": profileRejected, "logins": len(loginMS), "login": stats(loginMS), "login_rejected": loginRejected, "database": after.delta(before)})
		t.Logf("MIXED_AUTH_DB %s", authOut)
	}
	out, _ := json.Marshal(map[string]any{"variant": os.Getenv("PERF_AB_VARIANT"), "workers": workers, "warm_latest": warmLatest, "batch_size": batchSize, "sos_count": len(sosMS), "sos": stats(sosMS), "health_count": len(healthMS), "health": stats(healthMS), "health_rejected": rejected, "health_samples_per_second": float64(changes) / elapsed, "elapsed_seconds": elapsed, "pool_wait_count": pool.Stats().WaitCount, "pool_wait_ms": float64(pool.Stats().WaitDuration) / float64(time.Millisecond)})
	t.Logf("MIXED_SOS_AB %s", out)
}
