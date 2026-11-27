package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/session"
)

func TestSensitiveAuditBatchesBeforeDisclosure(t *testing.T) {
	pool := dbtest.Fixture(t)
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	pool.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var status string
	var insertsBefore uint64
	if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_insert'").Scan(&status, &insertsBefore); err != nil {
		t.Fatal(err)
	}
	var disclosed atomic.Int32
	h := sensitiveCareAudit(pool, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var n int
		if err := pool.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM care_sensitive_access_audit WHERE actor_id=?", session.FromContext(r).Principal.ID).Scan(&n); err != nil || n != 1 {
			t.Errorf("disclosure before audit: count=%d err=%v", n, err)
		}
		disclosed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	const requests = 24
	before := pool.Stats().WaitCount
	started := make(chan struct{}, requests)
	results := make(chan int, requests)
	for i := range requests {
		sess := store.Create(session.Principal{Role: "ADMIN", ID: int64(i + 1)})
		go func() {
			r := session.WithContext(httptest.NewRequest("POST", "/api/v1/health/daily-query", nil).WithContext(ctx), sess)
			w := httptest.NewRecorder()
			started <- struct{}{}
			h.ServeHTTP(w, r)
			results <- w.Code
		}()
	}
	for range requests {
		<-started
	}
	// While the sole connection is held, all requests must wait outside
	// database/sql except the active audit batch.
	deadline := time.Now().Add(150 * time.Millisecond)
	for pool.Stats().WaitCount == before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	waiters := pool.Stats().WaitCount - before
	if disclosed.Load() != 0 {
		t.Fatal("request disclosed while database unavailable")
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	for range requests {
		select {
		case status := <-results:
			if status != http.StatusOK {
				t.Fatal("audit response", status)
			}
		case <-ctx.Done():
			t.Fatal("audit requests did not complete")
		}
	}
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_sensitive_access_audit").Scan(&n); err != nil || n != requests || disclosed.Load() != requests {
		t.Fatal("audit parity", n, disclosed.Load(), err)
	}
	var insertsAfter uint64
	if err := pool.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_insert'").Scan(&status, &insertsAfter); err != nil {
		t.Fatal(err)
	}
	t.Logf("audit rows=%d SQL INSERTs=%d connection waiters=%d", n, insertsAfter-insertsBefore, waiters)
	if insertsAfter-insertsBefore >= requests {
		t.Fatal("audit writes were not coalesced")
	}
	if waiters != 1 {
		t.Fatalf("audit requests occupied %d connection waiters, want one active batch", waiters)
	}
}
