package handlers

import (
	"net/http"
	"runtime"
	"sync"
	"time"

	"yihe-care/internal/observability"
	"yihe-care/internal/response"
)

// Process-wide authentication budgets survive handler/database replacement.
// Do not queue expensive authentication work on a saturated CPU.
var loginAdmission = sync.OnceValue(func() chan struct{} {
	return make(chan struct{}, min(8, max(1, runtime.GOMAXPROCS(0)/2)))
})

func tryAdmission(slots chan struct{}) (func(), bool) {
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		return nil, false
	}
}

// Serialize concurrent login attempts for one validated phone. This prevents
// requests from racing the existing durable failure-count checks locally.
// The map contains only in-flight attempts and is bounded by the login gate.
type accountAdmission struct {
	mu     sync.Mutex
	active map[string]struct{}
}

var accountLogins = accountAdmission{active: make(map[string]struct{})}

func (a *accountAdmission) acquire(phone string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.active[phone]; exists {
		return false
	}
	a.active[phone] = struct{}{}
	return true
}

func (a *accountAdmission) release(phone string) {
	a.mu.Lock()
	delete(a.active, phone)
	a.mu.Unlock()
}

type bootstrapBudget struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

const bootstrapBurst = 64
const bootstrapRate = 10 // new anonymous sessions per second, per process

var anonymousSessions = bootstrapBudget{tokens: bootstrapBurst}
var bootstrapSlots = make(chan struct{}, 4)

// Limit public session reads before CSRF can consult the persistent store.
// Keep this budget independent of password verification and device/SOS traffic.
var sessionReadSlots = make(chan struct{}, 8)

// AuthenticationAdmission must wrap the CSRF/auth stack, after the per-IP
// limiter. A saturated request never enters session storage or credential
// lookup. GET patterns also accept HEAD in net/http's ServeMux.
func AuthenticationAdmission(next http.Handler) http.Handler {
	return authenticationAdmission(next, loginAdmission(), sessionReadSlots)
}

func authenticationAdmission(next http.Handler, login, reads chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var slots chan struct{}
		var metric string
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			slots, metric = login, "yihe_auth_login_busy_total"
		case (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/api/v1/auth/session":
			slots, metric = reads, "yihe_auth_session_read_busy_total"
		default:
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		release, ok := tryAdmission(slots)
		if !ok {
			authenticationBusy(w, metric)
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

// A rate token is spent only after a writer slot is available. Concurrent
// overload must not drain future anonymous-session admission.
func admitAnonymousSession() (func(), bool) {
	return admitBootstrap(bootstrapSlots, &anonymousSessions, time.Now())
}

func admitBootstrap(slots chan struct{}, budget *bootstrapBudget, now time.Time) (func(), bool) {
	release, ok := tryAdmission(slots)
	if !ok {
		return nil, false
	}
	if !budget.allow(now) {
		release()
		return nil, false
	}
	return release, true
}

func (b *bootstrapBudget) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() && now.After(b.last) {
		b.tokens = min(bootstrapBurst, b.tokens+now.Sub(b.last).Seconds()*bootstrapRate)
	}
	if b.last.IsZero() || now.After(b.last) {
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func authenticationBusy(w http.ResponseWriter, metric string) {
	observability.Default.Add(metric, 1)
	w.Header().Set("Retry-After", "1")
	response.JSON(w, http.StatusTooManyRequests, map[string]any{"message": "认证服务繁忙，请稍后重试"})
}
