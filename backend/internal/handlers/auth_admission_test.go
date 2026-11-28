package handlers

import (
	"net/http"
	"net/http/httptest"

	"sync"
	"sync/atomic"
	"testing"
	"time"
	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

func TestAuthenticationAdmissionBounded(t *testing.T) {
	slots := make(chan struct{}, 2)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	releaseAll := make(chan struct{})
	ready := make(chan struct{}, 32)
	for range 32 {
		wg.Go(func() {
			release, ok := tryAdmission(slots)
			if ok {
				accepted.Add(1)
			}
			ready <- struct{}{}
			if ok {
				<-releaseAll
				release()
			}
		})
	}
	for range 32 {
		<-ready
	}
	if got := accepted.Load(); got != 2 {
		t.Errorf("admitted %d, want 2", got)
	}
	close(releaseAll)
	wg.Wait()
	if release, ok := tryAdmission(slots); !ok {
		t.Fatal("admission not released")
	} else {
		release()
	}
	w := httptest.NewRecorder()
	authenticationBusy(w, "yihe_test_auth_busy_total")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatal("invalid overload response")
	}
}

func TestAuthenticationAdmissionBeforeSessionStorage(t *testing.T) {
	login, reads := make(chan struct{}, 1), make(chan struct{}, 1)
	login <- struct{}{}
	reads <- struct{}{}
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})
	stack := authenticationAdmission(next, login, reads)
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/auth/login"}, {"GET", "/api/v1/auth/session"}, {"HEAD", "/api/v1/auth/session"},
	} {
		w := httptest.NewRecorder()
		stack.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != 429 || w.Header().Get("Retry-After") != "1" || w.Header().Get("Cache-Control") != "no-store" || calls != 0 {
			t.Fatalf("saturated %s %s reached downstream or missed overload contract", tc.method, tc.path)
		}
	}
	for _, path := range []string{"/api/v1/device/sos", "/api/v1/auth/logout", "/healthz"} {
		w := httptest.NewRecorder()
		stack.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
		if w.Code != 204 {
			t.Fatalf("unrelated route %s blocked", path)
		}
	}
	<-login
	w := httptest.NewRecorder()
	stack.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/login", nil))
	if w.Code != 204 || len(login) != 0 || len(reads) != 1 {
		t.Fatal("session congestion affected login or leaked a slot")
	}
	<-reads
	// The real CSRF middleware must still reject an admitted request.
	store := session.NewStore(time.Hour)
	defer store.Close()
	w = httptest.NewRecorder()
	authenticationAdmission(csrf.Middleware(store, auth.Middleware(store, next)), login, reads).
		ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/auth/login", nil))
	if w.Code != 403 || len(login) != 0 {
		t.Fatal("CSRF bypassed or admission leaked")
	}
}

func TestAuthenticationAccountAdmission(t *testing.T) {
	a := accountAdmission{active: make(map[string]struct{})}
	if !a.acquire("13800000001") || a.acquire("13800000001") || !a.acquire("13800000002") {
		t.Fatal("account contention is not isolated")
	}
	a.release("13800000001")
	if !a.acquire("13800000001") {
		t.Fatal("account not released")
	}
	a.release("13800000001")
	a.release("13800000002")
	if len(a.active) != 0 {
		t.Fatal("account entries leaked")
	}
}

func TestBootstrapAdmissionDoesNotWasteRateTokens(t *testing.T) {
	slots := make(chan struct{}, 1)
	budget := bootstrapBudget{tokens: 1}
	now := time.Unix(100, 0)
	slots <- struct{}{}
	if _, ok := admitBootstrap(slots, &budget, now); ok {
		t.Fatal("busy write slot admitted")
	}
	<-slots
	release, ok := admitBootstrap(slots, &budget, now)
	if !ok {
		t.Fatal("busy rejection consumed the token")
	}
	release()
	if _, ok := admitBootstrap(slots, &budget, now); ok || len(slots) != 0 {
		t.Fatal("rate rejection leaked slot")
	}
}

func TestAnonymousBootstrapBudget(t *testing.T) {
	b := bootstrapBudget{tokens: bootstrapBurst}
	now := time.Unix(100, 0)
	for range bootstrapBurst {
		if !b.allow(now) {
			t.Fatal("premature denial")
		}
	}
	if b.allow(now) || b.allow(now.Add(-time.Second)) {
		t.Fatal("budget exceeded")
	}
	if !b.allow(now.Add(time.Second/bootstrapRate)) || b.allow(now.Add(time.Second/bootstrapRate)) {
		t.Fatal("refill incorrect")
	}
	for range bootstrapBurst {
		if !b.allow(now.Add(time.Hour)) {
			t.Fatal("burst refill incorrect")
		}
	}
	if b.allow(now.Add(time.Hour)) {
		t.Fatal("burst limit exceeded")
	}
}
