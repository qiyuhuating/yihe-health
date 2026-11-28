package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

// Exercise admission against real database/sql contention. The normal
// business connection is deliberately occupied, not a simulated handler.
func TestAuthOverloadLeavesReservedSOSConnection(t *testing.T) {
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	h.Sessions.UseDatabase(pool.DB, h.Box)
	sess := h.Sessions.Create(session.Principal{})
	if sess == nil {
		t.Fatal("anonymous fixture")
	}
	mux := http.NewServeMux()
	h.Routes(mux)
	app := AuthenticationAdmission(csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, h.EnforceAdminDuty(h.EnforceDeviceSignatures(mux)))))
	if pool.SOSPool() == pool.DB {
		t.Fatal("SOS pool is not independent")
	}
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(t.Context())
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	request := func(method, path, body string) *http.Request {
		r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
		r.Header.Set(csrf.HeaderName, sess.CSRFToken)
		return r
	}
	nReads, nLogins := cap(sessionReadSlots), cap(loginAdmission())
	before := pool.Stats().WaitCount
	for range nReads {
		workers.Go(func() { app.ServeHTTP(httptest.NewRecorder(), request("GET", "/api/v1/auth/session", "")) })
	}
	for range nLogins {
		workers.Go(func() {
			app.ServeHTTP(httptest.NewRecorder(), request("POST", "/api/v1/auth/login", `{"phone":"13800000001","password":"TestPassword123!"}`))
		})
	}
	deadline := time.Now().Add(2 * time.Second)
	for pool.Stats().WaitCount-before < int64(nReads+nLogins) {
		if time.Now().After(deadline) {
			t.Fatal("admitted requests did not reach the occupied business pool")
		}
		time.Sleep(time.Millisecond)
	}
	for _, r := range []*http.Request{request("GET", "/api/v1/auth/session", ""), request("POST", "/api/v1/auth/login", "{}")} {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
			t.Fatalf("saturated admission failed: %d %s", w.Code, w.Body.String())
		}
	}
	if got := pool.Stats().WaitCount - before; got != int64(nReads+nLogins) {
		t.Fatalf("rejected requests entered SQL pool: %d", got)
	}
	payload, err := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "auth-pool-sos", "bindingVersion": 1})
	if err != nil {
		t.Fatal(err)
	}
	sosCtx, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	r := httptest.NewRequest("POST", "/api/v1/device/sos", strings.NewReader(string(payload))).WithContext(sosCtx)
	w := httptest.NewRecorder()
	start := time.Now()
	app.ServeHTTP(w, r)
	elapsed := time.Since(start)
	var out map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out["returnCode"] != "1111" {
		t.Fatalf("SOS blocked: %d %s", w.Code, w.Body.String())
	}
	var count int
	if err = pool.SOSPool().QueryRowContext(sosCtx, "SELECT COUNT(*) FROM alarm_precess").Scan(&count); err != nil || count != 1 {
		t.Fatalf("SOS did not commit: %d %v", count, err)
	}
	t.Logf("occupied business pool, %d session reads and %d login requests waiting; further admission rejected without SQL; SOS durable commit in %v", nReads, nLogins, elapsed)
}
