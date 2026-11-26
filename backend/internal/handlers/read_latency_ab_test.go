package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/config"
	"yihe-care/internal/csrf"
	"yihe-care/internal/health"
	"yihe-care/internal/observability"
	"yihe-care/internal/ratelimit"
	"yihe-care/internal/session"
)

// Audit fixture only. It leaves production source unchanged, uses dbtest's
// disposable database and validates every response outside latency timing.
func TestReadLatencyAudit(t *testing.T) {
	if os.Getenv("RUN_READ_LATENCY_AUDIT") != "1" {
		t.Skip("opt-in isolated audit")
	}
	if os.Getenv("TEST_MYSQL_DSN") == "" {
		t.Fatal("TEST_MYSQL_DSN is required for the real MySQL read latency audit")
	}
	pool := testDatabase(t)
	ctx := context.Background()
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prior)
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	cfg := config.Config{DataKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), HealthCacheBytes: 16 << 20}
	h := New(pool, store, cfg)
	store.UseDatabase(pool.DB, h.Box)
	password := "AuditPassword123!"
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,user_phone,password,role) VALUES(1,'audit','13800000001',?,'1')", hash)
	seedDevice(t, pool)
	for i := 1; i <= 30; i++ {
		mustExec(t, pool, "INSERT INTO family(family_id,family_name) VALUES(?,'synthetic')", i)
		mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name,customer_age) VALUES(?,'synthetic',40)", i)
		mustExec(t, pool, "INSERT INTO text(title,text_main,time) VALUES('synthetic','synthetic',CURRENT_TIMESTAMP(6))")
		if i > 1 {
			mustExec(t, pool, "INSERT INTO oldperson(old_id,name,family_id) VALUES(?,'synthetic',?)", i, i)
			mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(?,?,'synthetic-only',?)", i, fmt.Sprintf("AUDIT%03d", i), i)
		}
	}
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	for i := 0; i < 100; i++ {
		mustExec(t, pool, "INSERT INTO message_outbox(topic,source_key,family_id,channel,recipient) VALUES('audit',?,1,'in_app','synthetic')", fmt.Sprint(i))
	}
	from := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	for start := 0; start < 10000; start += 200 {
		samples := make([]health.Sample, 200)
		for i := range samples {
			at := from.Add(time.Duration(start+i) * time.Second)
			samples[i] = health.Sample{RecordID: fmt.Sprint(start + i), Source: "watch", SourceDevice: "AUDIT", Metric: "heart_rate", Semantics: "instant", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
		}
		tx, e := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if e != nil {
			t.Fatal(e)
		}
		_, duplicates, e := health.SaveBatch(ctx, tx, health.Owner{OldID: 1}, samples)
		if e != nil || duplicates != 0 {
			tx.Rollback()
			t.Fatalf("seed: duplicates=%d err=%v", duplicates, e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	var saved, events int
	if err = pool.QueryRowContext(ctx, "SELECT count(*) FROM health_sample").Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRowContext(ctx, "SELECT count(*) FROM health_change").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if saved != 10000 || events != 10000 {
		t.Fatalf("seed counts %d/%d", saved, events)
	}
	sess := store.Create(session.Principal{Role: auth.RoleAdmin, ID: 1})
	if sess == nil {
		t.Fatal("session creation failed")
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	mux.HandleFunc("POST /api/v1/audit/probe", func(w http.ResponseWriter, r *http.Request) {
		if auth.Principal(r) == nil {
			t.Error("missing principal")
		}
		w.Write([]byte(`{"ok":true}`))
	})
	registry := observability.New()
	registry.SetRouteResolver(func(r *http.Request) string { _, pattern := mux.Handler(r); return pattern })
	limiter := ratelimit.New()
	t.Cleanup(limiter.Close)
	stack := registry.HTTP(limiter.Middleware(csrf.Middleware(store, auth.Middleware(store, mux))))
	server := httptest.NewServer(stack)
	defer server.Close()
	client := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConns: 8, MaxIdleConnsPerHost: 8}}
	defer client.CloseIdleConnections()
	type result struct {
		Name      string  `json:"name"`
		Mode      string  `json:"mode"`
		Round     int     `json:"round"`
		N         int     `json:"n"`
		P50       float64 `json:"p50_ms"`
		P95       float64 `json:"p95_ms"`
		P99       float64 `json:"p99_ms"`
		Max       float64 `json:"max_ms"`
		Alloc     uint64  `json:"allocated_bytes_per_operation"`
		Errors    int     `json:"errors"`
		LatencyNS []int64 `json:"latency_ns"`
	}
	results := []result{}
	measure := func(name, mode string, round, n int, op func() time.Duration) {
		for i := 0; i < 3; i++ {
			op()
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		raw := make([]int64, n)
		for i := range raw {
			raw[i] = int64(op())
		}
		runtime.ReadMemStats(&after)
		sorted := append([]int64(nil), raw...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		p := func(q float64) float64 { return float64(sorted[int(math.Ceil(q*float64(n)))-1]) / 1e6 }
		r := result{name, mode, round, n, p(.50), p(.95), p(.99), float64(sorted[n-1]) / 1e6, (after.TotalAlloc - before.TotalAlloc) / uint64(n), 0, raw}
		results = append(results, r)
		t.Logf("AUDIT %s %s round=%d n=%d p50=%.4f p95=%.4f p99=%.4f ms", name, mode, round, n, r.P50, r.P95, r.P99)
	}
	type endpoint struct {
		name, path string
		body       any
		key        string
		count      int
		cold       bool
	}
	page := map[string]any{"page": 1, "pageSize": 20, "name": ""}
	stats := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "AUDIT", "metric": "heart_rate", "semantics": "instant", "from": from, "to": from.Add(24 * time.Hour)}
	endpoints := []endpoint{
		{"authenticated_probe", "/api/v1/audit/probe", map[string]any{}, "", 0, false},
		{"text/select", "/api/v1/notices/list", page, "texts", 20, false},
		{"customer/select", "/api/v1/guardians/list", page, "customers", 20, false},
		{"oldPerson/select", "/api/v1/residents/list", page, "oldPeople", 20, false},
		{"family/select", "/api/v1/family/select", page, "familyDtos", 20, false},
		{"device/list", "/api/v1/device/list", page, "devices", 20, false},
		{"notification/list", "/api/v1/notifications/search", map[string]any{"familyId": 1}, "items", 100, false},
		{"health/changes", "/api/v1/health/changes", map[string]any{"oldId": 1, "limit": 100}, "changes", 100, false},
		{"statistics_hot", "/api/v1/health/statistics", stats, "statistics", 10000, false},
		{"statistics_uncached", "/api/v1/health/statistics", stats, "statistics", 10000, true},
	}
	for _, ep := range endpoints {
		body, e := json.Marshal(ep.body)
		if e != nil {
			t.Fatal(e)
		}
		if ep.cold {
			h.healthCache = nil
		} else if ep.key == "statistics" {
			h.healthCache = newStatisticsCache(16 << 20)
		}
		for _, mode := range []string{"in_process", "loopback_http"} {
			op := func() time.Duration {
				var data []byte
				var status int
				var elapsed time.Duration
				req, e := http.NewRequest(http.MethodPost, server.URL+ep.path, bytes.NewReader(body))
				if e != nil {
					t.Fatal(e)
				}
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
				req.Header.Set(csrf.HeaderName, sess.CSRFToken)
				if mode == "in_process" {
					w := httptest.NewRecorder()
					start := time.Now()
					stack.ServeHTTP(w, req)
					elapsed = time.Since(start)
					status = w.Code
					data = w.Body.Bytes()
				} else {
					start := time.Now()
					resp, e := client.Do(req)
					if e != nil {
						t.Fatal(e)
					}
					data, e = io.ReadAll(resp.Body)
					resp.Body.Close()
					elapsed = time.Since(start)
					if e != nil {
						t.Fatal(e)
					}
					status = resp.StatusCode
				}
				if status != 200 {
					t.Fatalf("%s HTTP %d: %s", ep.name, status, data)
				}
				var out map[string]any
				if json.Unmarshal(data, &out) != nil {
					t.Fatalf("invalid JSON %s", ep.name)
				}
				if code, exists := out["returnCode"]; exists && code != "1111" {
					t.Fatalf("%s business error: %s", ep.name, data)
				}
				if ep.key == "statistics" {
					s, ok := out[ep.key].(map[string]any)
					if !ok || s["count"] != float64(ep.count) || s["mean"] != float64(72) {
						t.Fatalf("bad statistics: %s", data)
					}
				} else if ep.key != "" {
					a, ok := out[ep.key].([]any)
					if !ok || len(a) != ep.count {
						t.Fatalf("%s bad count: %s", ep.name, data)
					}
				} else if out["ok"] != true {
					t.Fatal("probe failed")
				}
				return elapsed
			}
			n := 300
			if mode == "in_process" || ep.cold {
				n = 40
			}
			for round := 1; round <= 3; round++ {
				measure(ep.name, mode, round, n, op)
			}
		}
	}
	for _, mode := range []string{"database", "memory_only_no_cross_replica_revocation"} {
		ss := store
		s := sess
		if mode != "database" {
			ss = session.NewStore(time.Hour)
			t.Cleanup(ss.Close)
			s = ss.Create(session.Principal{Role: auth.RoleAdmin, ID: 1})
		}
		for round := 1; round <= 3; round++ {
			measure("session.Get", mode, round, 1000, func() time.Duration {
				start := time.Now()
				got := ss.Get(s.ID)
				elapsed := time.Since(start)
				if got == nil || got.Principal.ID != 1 {
					t.Fatal("session invalid")
				}
				return elapsed
			})
		}
	}
	loginBody, _ := json.Marshal(map[string]any{"phone": "13800000001", "password": password})
	measure("login", "loopback_http", 1, 20, func() time.Duration {
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/login", bytes.NewReader(loginBody))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
		req.Header.Set(csrf.HeaderName, sess.CSRFToken)
		start := time.Now()
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		data, e := io.ReadAll(resp.Body)
		resp.Body.Close()
		elapsed := time.Since(start)
		if e != nil || resp.StatusCode != 200 || !bytes.Contains(data, []byte(`"authenticated":true`)) {
			t.Fatalf("login: %d %s %v", resp.StatusCode, data, e)
		}
		var next *session.Session
		for _, cookie := range resp.Cookies() {
			if cookie.Name == session.CookieName {
				next = store.Get(cookie.Value)
			}
		}
		if next == nil {
			t.Fatal("login session missing")
		}
		sess = next
		return elapsed
	})
	if profilePath := os.Getenv("AUDIT_CPU_PROFILE"); profilePath != "" {
		f, e := os.Create(profilePath)
		if e != nil {
			t.Fatal(e)
		}
		if e = pprof.StartCPUProfile(f); e != nil {
			t.Fatal(e)
		}
		defer f.Close()
		defer pprof.StopCPUProfile()
	}
	measure("bcrypt_verify_cost12", "in_process", 1, 20, func() time.Duration {
		start := time.Now()
		ok := auth.Matches(password, hash)
		elapsed := time.Since(start)
		if !ok {
			t.Fatal("password mismatch")
		}
		return elapsed
	})
	peer := session.NewStore(time.Hour)
	t.Cleanup(peer.Close)
	peer.UseDatabase(pool.DB, h.Box)
	if !peer.Destroy(sess.ID) || store.Get(sess.ID) != nil {
		t.Fatal("cross-replica revoke failed")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notices/list", bytes.NewReader([]byte(`{}`)))
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	req.Header.Set(csrf.HeaderName, sess.CSRFToken)
	w := httptest.NewRecorder()
	stack.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("revoked session HTTP %d", w.Code)
	}
	if err = pool.QueryRowContext(ctx, "SELECT count(*) FROM login_audit WHERE outcome='success'").Scan(&saved); err != nil || saved != 23 {
		t.Fatalf("login count=%d err=%v", saved, err)
	}
	buildSettings := map[string]string{}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if strings.HasPrefix(setting.Key, "vcs.") {
				buildSettings[setting.Key] = setting.Value
			}
		}
	}
	report := map[string]any{"baseline_commit": "daac58a9d20420cc4bdf71675be64bcc20cc8e49", "variant": os.Getenv("PERF_AB_VARIANT"), "build_settings": buildSettings, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "gomaxprocs": runtime.GOMAXPROCS(0), "results": results, "health_samples": 10000, "health_events": events, "successful_logins": saved, "revocation_check": "pass", "session_cache": "database store has no read cache; memory-only comparison is separate semantics", "middleware": "metrics and formatted logging(io.Discard), rate limit, CSRF, auth, routes; no outer service/static/proxy/TLS/background jobs", "cold_definition": "application statistics cache disabled; MySQL buffer pool warm", "allocation_scope": "whole process including client/validation; not server-only"}
	data, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(os.Getenv("READ_LATENCY_RESULT_PATH"), data, 0644); e != nil {
		t.Fatal(e)
	}
}
