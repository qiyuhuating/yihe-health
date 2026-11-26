package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/session"
)

func TestCapabilityReadsAreAudited(t *testing.T) {
	pool := dbtest.MySQL(t)
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	sess := store.Create(session.Principal{Role: "ADMIN", ID: 1})
	calls := 0
	h := sensitiveCareAudit(pool, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusOK) }))
	for _, path := range []string{"/api/v1/fence-events/search", "/api/v1/alert-reviews/search", "/api/v1/health/daily-query"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"secret":"must-not-be-audited"}`))
		r = session.WithContext(r, sess)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM care_sensitive_access_audit WHERE actor_id=1").Scan(&count); err != nil || count != 3 || calls != 3 {
		t.Fatal(count, calls, err)
	}
	pool.Close()
	r := session.WithContext(httptest.NewRequest("POST", "/api/v1/fence-events/search", nil), sess)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 503 || calls != 3 {
		t.Fatal("audit failure must prevent disclosure", w.Code, calls)
	}
}
