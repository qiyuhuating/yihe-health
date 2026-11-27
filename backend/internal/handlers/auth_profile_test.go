package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

func TestAdminSessionProfileAuthorization(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedCareAdminDuty(t, pool, 41, "supervisor")
	h.Sessions.UseDatabase(pool.DB, h.Box)
	sess := h.Sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: 41})
	if sess == nil {
		t.Fatal("create session")
	}
	mux := http.NewServeMux()
	h.routeAuth(mux)
	app := AuthenticationAdmission(csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, h.EnforceAdminDuty(mux))))
	request := func(handler http.Handler) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/auth/session", nil)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	checkProfile := func(w *httptest.ResponseRecorder) {
		t.Helper()
		var body struct {
			Authenticated, SetupRequired bool
			Account                      struct{ Duty string }
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || !body.Authenticated || body.SetupRequired || body.Account.Duty != "supervisor" {
			t.Fatalf("profile %d %s", w.Code, w.Body.String())
		}
	}
	checkProfile(request(app))
	checkProfile(request(http.HandlerFunc(h.sessionInfo)))
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='caregiver' WHERE admin_id=41")
	if w := request(app); w.Code != 403 {
		t.Fatalf("demotion reused prior request: %d %s", w.Code, w.Body.String())
	}
	mustExec(t, pool, "UPDATE care_staff_permission SET duty='supervisor' WHERE admin_id=41")
	mustExec(t, pool, "UPDATE users SET account_status='suspended' WHERE user_id=41")
	if w := request(app); w.Code != 403 {
		t.Fatalf("disabled account passed gate: %d", w.Code)
	}
	w := request(http.HandlerFunc(h.sessionInfo))
	var body struct{ Authenticated, SetupRequired bool }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Authenticated || body.SetupRequired {
		t.Fatalf("standalone disabled account profile %d %s", w.Code, w.Body.String())
	}
}

// Opt-in: use only an otherwise idle disposable MySQL server. The same
// harness can run against another checkout; global counters are evidence,
// not assertions in normal CI. Timings exclude fixture setup and warmup.
func TestProfileComparison(t *testing.T) {
	if os.Getenv("YIHE_PROFILE_AB") != "1" {
		t.Skip("isolated comparison only")
	}
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedCareAdminDuty(t, pool, 41, "supervisor")
	h.Sessions.UseDatabase(pool.DB, h.Box)
	sess := h.Sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: 41})
	if sess == nil {
		t.Fatal("session")
	}
	mux := http.NewServeMux()
	h.routeAuth(mux)
	app := AuthenticationAdmission(csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, h.EnforceAdminDuty(mux))))
	selects := func() uint64 {
		t.Helper()
		var key, value string
		if err := pool.QueryRowContext(t.Context(), "SHOW GLOBAL STATUS LIKE 'Com_select'").Scan(&key, &value); err != nil {
			t.Fatal(err)
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	call := func(cookie string, want int) {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/auth/session", nil)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
		}
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("HTTP %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	for range cap(bootstrapSlots) {
		bootstrapSlots <- struct{}{}
	}
	defer func() {
		for range cap(bootstrapSlots) {
			<-bootstrapSlots
		}
	}()
	for _, tc := range []struct {
		name, cookie string
		status       int
	}{{"admin", sess.ID, 200}, {"miss", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 429}, {"no_cookie", "", 429}} {
		for range 32 {
			call(tc.cookie, tc.status)
		}
		before := selects()
		const n = 512
		times := make([]time.Duration, n)
		start := time.Now()
		for i := range times {
			begin := time.Now()
			call(tc.cookie, tc.status)
			times[i] = time.Since(begin)
		}
		elapsed := time.Since(start)
		count := selects() - before
		slices.Sort(times)
		t.Logf("PROFILE %s n=%d select=%d rps=%.2f p50_us=%.3f p99_us=%.3f", tc.name, n, count, float64(n)/elapsed.Seconds(), float64(times[n/2])/1000, float64(times[(n*99+99)/100-1])/1000)
	}
}
