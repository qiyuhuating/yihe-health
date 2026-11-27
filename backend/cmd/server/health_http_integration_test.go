package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/config"
	"yihe-care/internal/csrf"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/devicecredential"
	"yihe-care/internal/handlers"
	"yihe-care/internal/ratelimit"
	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

func TestHealthHTTPAuditAndSOSUnderPoolSaturation(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "0")
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "")
	pool := dbtest.MySQL(t)
	ctx := t.Context()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	const secret = "test-device-secret-1234567890"
	hash, err := devicecredential.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO users(user_id,user_name,role) VALUES(11,'synthetic','1')")
	exec("INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(11,'supervisor',11)")
	exec("INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')")
	exec("INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'WATCH001',?,1)", hash)

	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	h := handlers.New(pool, store, config.Config{DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), HealthCacheBytes: 4 << 20})
	store.UseDatabase(pool.DB, h.Box)
	sess := store.Create(session.Principal{Role: auth.RoleAdmin, ID: 11})
	if sess == nil {
		t.Fatal("could not create persisted session")
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	limiter := ratelimit.New()
	defer limiter.Close()
	srv := httptest.NewServer(limiter.Middleware(csrf.Middleware(store, auth.Middleware(store,
		h.EnforceAdminDuty(h.EnforceDeviceSignatures(sensitiveCareAudit(pool, response.Router(mux))))))))
	defer srv.Close()
	client := srv.Client()
	client.Timeout = 5 * time.Second
	const dailyPath = "/api/v1/health/daily-query"
	daily := []byte(`{"oldId":1,"source":"watch","sourceDevice":"WATCH001","metric":"heart_rate","semantics":"instant","day":"2026-10-22","days":1}`)
	post := func(path string, body []byte, account, token bool) (int, map[string]any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if account {
			req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
		}
		if token {
			req.Header.Set(csrf.HeaderName, sess.CSRFToken)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		var out map[string]any
		err = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out, err
	}
	requireDaily := func() error {
		status, out, err := post(dailyPath, daily, true, true)
		if err != nil || status != http.StatusOK || out["returnCode"] != "1111" {
			return fmt.Errorf("daily: HTTP %d %v %v", status, out, err)
		}
		return nil
	}
	// One physical business connection gives per-session statement counts;
	// the independent SOS pool remains available during deliberate saturation.
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	if err := requireDaily(); err != nil {
		t.Fatal(err)
	}
	status := func() map[string]int64 {
		t.Helper()
		rows, err := pool.QueryContext(ctx, "SHOW SESSION STATUS WHERE Variable_name IN ('Com_select','Com_insert','Com_begin','Com_commit','Questions','Com_stmt_execute','Com_stmt_prepare')")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		values := make(map[string]int64)
		for rows.Next() {
			var key string
			var value int64
			if err := rows.Scan(&key, &value); err != nil {
				t.Fatal(err)
			}
			values[key] = value
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return values
	}
	before := status()
	var reads []time.Duration
	for range 10 {
		start := time.Now()
		if err := requireDaily(); err != nil {
			t.Fatal(err)
		}
		reads = append(reads, time.Since(start))
	}
	after := status()
	for k, v := range after {
		t.Logf("HTTP daily warm %s delta=%d (10 requests)", k, v-before[k])
	}
	// Session lookup, coarse account access, locked duty and cache read.
	// Warm daily rows no longer lock the owner clock. The only insert is
	// the mandatory sensitive-access audit.
	if after["Com_select"]-before["Com_select"] != 40 || after["Com_insert"]-before["Com_insert"] != 10 || after["Com_commit"]-before["Com_commit"] != 10 {
		t.Fatalf("unexpected daily statements before=%v after=%v", before, after)
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i] < reads[j] })
	t.Logf("HTTP daily warm n=%d median=%s max=%s", len(reads), reads[len(reads)/2], reads[len(reads)-1])
	if code, _, err := post(dailyPath, daily, true, false); err != nil || code != http.StatusForbidden {
		t.Fatalf("missing CSRF: %d %v", code, err)
	}

	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitBefore := pool.Stats().WaitCount
	const queued = 4
	var wg sync.WaitGroup
	errs := make(chan error, queued)
	for range queued {
		wg.Go(func() { errs <- requireDaily() })
	}
	deadline := time.Now().Add(time.Second)
	for pool.Stats().WaitCount < waitBefore+queued && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pool.Stats().WaitCount < waitBefore+queued {
		t.Fatal("ordinary requests did not saturate the connection pool")
	}
	var sos []time.Duration
	for i := range 5 {
		body, _ := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": secret, "bindingVersion": 1, "requestId": fmt.Sprintf("http-sos-%d", i)})
		start := time.Now()
		code, out, err := post("/api/v1/device/sos", body, false, false)
		sos = append(sos, time.Since(start))
		if err != nil || code != http.StatusOK || out["returnCode"] != "1111" {
			t.Fatalf("SOS while ordinary pool saturated: HTTP %d %v %v", code, out, err)
		}
	}
	sort.Slice(sos, func(i, j int) bool { return sos[i] < sos[j] })
	t.Logf("HTTP SOS during ordinary pool saturation n=%d median=%s max=%s queued=%d", len(sos), sos[len(sos)/2], sos[len(sos)-1], queued)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var audit, incidents int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_sensitive_access_audit WHERE actor_id=11 AND resource=?", dailyPath).Scan(&audit); err != nil || audit != 11+queued {
		t.Fatalf("persisted access audit count=%d err=%v", audit, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM alarm_precess").Scan(&incidents); err != nil || incidents != 5 {
		t.Fatalf("persisted SOS count=%d err=%v", incidents, err)
	}
	if err := store.Revoke(auth.RoleAdmin, 11); err != nil {
		t.Fatal(err)
	}
	if code, _, err := post(dailyPath, daily, true, true); err != nil || code != http.StatusForbidden {
		t.Fatalf("revoked session: HTTP %d %v", code, err)
	}
}
