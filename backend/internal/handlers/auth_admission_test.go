package handlers

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
