package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/session"
)

func TestSensitiveReadBudgetIsolationAndRollover(t *testing.T) {
	var b sensitiveReadBudget
	now := time.Now()
	a := session.Principal{Role: "ADMIN", ID: 1}
	f := session.Principal{Role: "FAMILY", ID: 1}
	for range readBudgetPerActor {
		if !b.acquire(a, now) {
			t.Fatal("initial admission denied")
		}
	}
	if b.acquire(a, now) {
		t.Fatal("actor concurrency exceeded")
	}
	if !b.acquire(f, now) {
		t.Fatal("independent role blocked")
	}
	b.release(f)
	if b.acquire(a, now.Add(time.Minute)) {
		t.Fatal("rollover reset active work")
	}
	b.release(a)
	b.release(a)
	for range readBudgetPerAccount {
		if !b.acquire(a, now.Add(time.Minute)) {
			t.Fatal("premature rate denial")
		}
		b.release(a)
	}
	if b.acquire(a, now.Add(time.Minute)) {
		t.Fatal("rate budget exceeded")
	}
	if !b.acquire(f, now.Add(time.Minute)) {
		t.Fatal("unrelated actor throttled")
	}
	b.release(f)
	if !b.acquire(a, now.Add(2*time.Minute)) {
		t.Fatal("expired budget retained")
	}
	b.release(a)
}

func TestSensitiveReadBudgetGlobalCapacity(t *testing.T) {
	var b sensitiveReadBudget
	now := time.Now()
	for i := range readBudgetParallel {
		if !b.acquire(session.Principal{Role: "ADMIN", ID: int64(i + 1)}, now) {
			t.Fatal("premature global denial")
		}
	}
	p := session.Principal{Role: "FAMILY", ID: 1}
	if b.acquire(p, now) {
		t.Fatal("global capacity exceeded")
	}
	b.release(session.Principal{Role: "ADMIN", ID: 1})
	if !b.acquire(p, now) {
		t.Fatal("capacity not recovered")
	}
	b.release(p)
}

func TestSensitiveReadBudgetExcludesEmergencyEndpoints(t *testing.T) {
	for _, path := range []string{"/api/v1/device/sos", "/api/v1/device/healthBatch", "/api/v1/device/heartbeat", "/api/v1/care/transition"} {
		if costlySensitiveRead(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Fatal("emergency or write path limited", path)
		}
	}
	for _, path := range []string{"/api/v1/health/changes", "/api/v1/health/statistics", "/api/v1/vitals/history", "/api/v1/residents/list"} {
		if !costlySensitiveRead(httptest.NewRequest(http.MethodPost, path, nil)) {
			t.Fatal("missing sensitive read", path)
		}
	}
}
