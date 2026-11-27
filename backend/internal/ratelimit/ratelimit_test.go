package ratelimit

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSlidingWindowDifferential(t *testing.T) {
	for _, limit := range []int{1, 30, 60, 1200} {
		queue := hitWindow{limit: limit}
		rng := rand.New(rand.NewSource(int64(limit)))
		now := time.Unix(1000, 0)
		var reference []time.Time
		for i := 0; i < 20000; i++ {
			if i%100 == 0 {
				now = now.Add(window)
			} else {
				now = now.Add(time.Duration(rng.Intn(4000)) * time.Millisecond)
			}
			kept := reference[:0]
			for _, at := range reference {
				if now.Sub(at) < window {
					kept = append(kept, at)
				}
			}
			want := len(kept) < limit
			if want {
				kept = append(kept, now)
			}
			reference = kept
			if got := queue.allowBudget(now, queue.limit); got != want || queue.size != len(reference) {
				t.Fatalf("limit=%d at=%d got=%v want=%v size=%d wantSize=%d", limit, i, got, want, queue.size, len(reference))
			}
			for j, at := range reference {
				if queue.times[(queue.head+j)%len(queue.times)] != at {
					t.Fatal("FIFO order changed")
				}
			}
		}
	}
}

func TestSlidingWindowExactBoundary(t *testing.T) {
	w := hitWindow{limit: 2}
	now := time.Now()
	if !w.allowBudget(now, w.limit) || !w.allowBudget(now.Add(time.Minute), w.limit) || w.allowBudget(now.Add(window-time.Nanosecond), w.limit) || !w.allowBudget(now.Add(window), w.limit) || w.allowBudget(now.Add(window), w.limit) || !w.allowBudget(now.Add(window+time.Minute), w.limit) {
		t.Fatal("sliding boundary or denied-hit accounting changed")
	}
}

func TestSlidingWindowSparseCapacityAndWrappedGrowth(t *testing.T) {
	now := time.Now()
	w := hitWindow{limit: 60}
	if !w.allowBudget(now, w.limit) || len(w.times) != 1 {
		t.Fatal("one-shot client allocated a full quota")
	}
	if !w.allowBudget(now.Add(time.Minute), w.limit) || len(w.times) != 2 {
		t.Fatal("unexpected initial growth")
	}
	// Expire the first hit, wrap into its slot, then grow the full wrapped ring.
	if !w.allowBudget(now.Add(window), w.limit) || w.head != 1 {
		t.Fatal("fixture did not wrap")
	}
	if !w.allowBudget(now.Add(window), w.limit) || len(w.times) != 4 || w.head != 0 || !w.times[0].Equal(now.Add(time.Minute)) {
		t.Fatal("wrapped growth lost chronological order")
	}
	for i := w.size; i < 60; i++ {
		if !w.allowBudget(now.Add(window), w.limit) {
			t.Fatal("premature denial")
		}
	}
	if len(w.times) != 60 || w.allowBudget(now.Add(window), w.limit) {
		t.Fatal("capacity exceeded quota")
	}
}

func TestLimiterConcurrentQuotaAndIsolation(t *testing.T) {
	l := New()
	t.Cleanup(l.Close)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	r.RemoteAddr = "[2001:db8::1]:12345"
	var accepted atomic.Int64
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for range 128 {
			l.sweep(time.Now())
		}
	}()
	for range 128 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if l.Allow(r) {
				accepted.Add(1)
			}
		}()
	}
	workers.Wait()
	if accepted.Load() != 30 {
		t.Fatalf("accepted %d; want 30", accepted.Load())
	}
	for _, tc := range []struct{ method, path, ip string }{
		{http.MethodPost, "/api/v1/auth/login", "[2001:db8::2]:12345"},
		{http.MethodPost, "/api/v1/device/login", r.RemoteAddr},
		{http.MethodGet, "/api/v1/auth/login", r.RemoteAddr},
		{http.MethodPost, "/api/v1/device/sos", r.RemoteAddr},
	} {
		other := httptest.NewRequest(tc.method, tc.path, nil)
		other.RemoteAddr = tc.ip
		if !l.Allow(other) {
			t.Fatalf("quota leaked to %+v", tc)
		}
	}
	r.Header.Set("X-Forwarded-For", "192.0.2.123")
	r.RemoteAddr = "[2001:db8::1]:54321"
	w := httptest.NewRecorder()
	l.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("denied request reached handler") })).ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") != "600" {
		t.Fatal("HTTP denial contract changed")
	}
}

func TestLimiterSweepAndClose(t *testing.T) {
	l := New()
	defer l.Close()
	now := time.Now()
	ip := "192.0.2.1"
	shard := l.shard(ip)
	expired, active := limitKey{ip, "expired"}, limitKey{ip, "active"}
	shard.hits[expired] = &hitWindow{times: []time.Time{now.Add(-window)}, size: 1, limit: 1, entry: shard.order.PushBack(expired)}
	// A wrapped FIFO with an old first hit and a recent last hit must survive.
	shard.hits[active] = &hitWindow{times: []time.Time{now, now.Add(-window)}, head: 1, size: 2, limit: 2, entry: shard.order.PushBack(active)}
	shard.slots = 3
	l.sweep(now)
	if shard.slots != 2 || shard.order.Len() != 1 {
		t.Fatal("reclaimed quota accounting mismatch")
	}
	if _, ok := shard.hits[expired]; ok {
		t.Fatal("expired key retained")
	}
	if _, ok := shard.hits[active]; !ok {
		t.Fatal("active key removed")
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() { defer workers.Done(); l.Close() }()
	}
	workers.Wait()
	select {
	case <-l.done:
	default:
		t.Fatal("janitor did not terminate")
	}
}

// Capacity cleanup must reclaim the actual oldest expired quota, not a random
// subset of active keys. Accepted hits move to the back; denials never do.
func TestLimiterReclaimsOldestAndPreservesActiveQuota(t *testing.T) {
	l, err := NewWithOptions(Options{MaxKeys: 32, MaxSlots: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/setup", nil)
	r.RemoteAddr = "192.0.2.1:42"
	if !l.Allow(r) {
		t.Fatal("initial quota")
	}
	s := l.shard("192.0.2.1")
	key := limitKey{"192.0.2.1", r.URL.Path}
	s.mu.Lock()
	s.hits[key].times[0] = time.Now().Add(-window)
	s.mu.Unlock()
	r.URL.Path = "/api/v1/auth/login"
	if !l.Allow(r) {
		t.Fatal("expired key did not return capacity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.hits) != 1 || s.order.Len() != 1 || s.slots != 1 {
		t.Fatalf("inconsistent quota budgets: keys=%d order=%d slots=%d", len(s.hits), s.order.Len(), s.slots)
	}
	if _, exists := s.hits[key]; exists {
		t.Fatal("expired quota retained")
	}
}

func TestLimiterExpiryFollowsAcceptedHitsOnly(t *testing.T) {
	l := New()
	t.Cleanup(l.Close)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/setup", nil)
	r.RemoteAddr = "192.0.2.1:42"
	if !l.Allow(r) {
		t.Fatal("first quota")
	}
	a := limitKey{"192.0.2.1", r.URL.Path}
	r.URL.Path = "/api/v1/auth/login"
	if !l.Allow(r) {
		t.Fatal("second quota")
	}
	b := limitKey{"192.0.2.1", r.URL.Path}
	r.URL.Path = a.path
	for range 29 {
		if !l.Allow(r) {
			t.Fatal("active quota prematurely denied")
		}
	}
	s := l.shard(a.ip)
	s.mu.Lock()
	if s.order.Front().Value.(limitKey) != b || s.order.Back().Value.(limitKey) != a {
		s.mu.Unlock()
		t.Fatal("accepted request did not update expiration order")
	}
	// Refresh the other key so the saturated quota is the oldest.
	s.mu.Unlock()
	r.URL.Path = b.path
	if !l.Allow(r) {
		t.Fatal("second quota refresh")
	}
	r.URL.Path = a.path
	if l.Allow(r) {
		t.Fatal("quota exceeded")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.order.Front().Value.(limitKey) != a || s.order.Back().Value.(limitKey) != b {
		t.Fatal("denial extended expiration")
	}
}
