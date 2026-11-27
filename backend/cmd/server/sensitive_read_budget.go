package main

import (
	"net/http"
	"sync"
	"time"

	"yihe-care/internal/session"
)

const (
	readBudgetPerAccount = 120
	readBudgetAccounts   = 4096
	readBudgetParallel   = 16
	readBudgetPerActor   = 2
)

type readActor struct {
	role string
	id   int64
}

// Keep the budget process-wide so a database reconnect cannot reset it.
// Separate active work from the fixed-window counters: a minute rollover
// must not release a slot still occupied by an in-flight request.
type sensitiveReadBudget struct {
	mu     sync.Mutex
	start  time.Time
	counts map[readActor]int
	active map[readActor]int
	total  int
}

var protectedReadBudget sensitiveReadBudget

func costlySensitiveRead(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/health/changes", "/api/v1/health/statistics", "/api/v1/health/daily-query",
		"/api/v1/vitals/history", "/api/v1/vitals/chart",
		"/api/v1/activity/history", "/api/v1/activity/chart",
		"/api/v1/residents/list", "/api/v1/family/select",
		"/api/v1/fence-events/search":
		return true
	}
	return false
}

func (b *sensitiveReadBudget) acquireCurrent(p session.Principal) bool {
	return b.acquire(p, time.Now())
}

func (b *sensitiveReadBudget) acquire(p session.Principal, now time.Time) bool {
	key := readActor{p.Role, p.ID}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.start.IsZero() || now.Sub(b.start) >= time.Minute {
		b.start = now
		b.counts = make(map[readActor]int)
	}
	if b.total >= readBudgetParallel || b.active[key] >= readBudgetPerActor {
		return false
	}
	n, exists := b.counts[key]
	if n >= readBudgetPerAccount || (!exists && len(b.counts) >= readBudgetAccounts) {
		return false
	}
	if b.active == nil {
		b.active = make(map[readActor]int)
	}
	b.counts[key] = n + 1
	b.active[key]++
	b.total++
	return true
}

func (b *sensitiveReadBudget) release(p session.Principal) {
	key := readActor{p.Role, p.ID}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total--
	if b.active[key] == 1 {
		delete(b.active, key)
	} else {
		b.active[key]--
	}
}
