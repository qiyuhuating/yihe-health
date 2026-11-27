package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	dbstore "yihe-care/internal/db"
	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

// Audit authenticated requests to endpoints that expose personal, health,
// location or device information. No request bodies, phone numbers, location
// coordinates, bearer tokens or query strings are stored.
func sensitiveCareAudit(pool *dbstore.Database, next http.Handler) http.Handler {
	prefixes := []string{
		"/api/v1/residents/", "/api/v1/family/", "/api/v1/position/", "/api/v1/locations/alerts/",
		"/api/v1/vitals/", "/api/v1/activity/", "/api/v1/device/",
		"/api/v1/monitoring-profiles/", "/api/v1/image/download",
		"/api/v1/fence/", "/api/v1/fence-events/", "/api/v1/alert/", "/api/v1/alert-reviews/", "/api/v1/health/daily-query", "/api/v1/health/alerts/",
		"/api/v1/security/",
		"/api/v1/notification/",
	}
	writer := sensitiveAuditWriter{pool: pool}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		audit := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				audit = true
				break
			}
		}
		// Public device telemetry endpoints do not carry a user session;
		// recording an anonymous actor here would be misleading.
		sess := session.FromContext(r)
		if !audit || sess == nil || sess.Principal.Role == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !writer.record(r.Context(), sensitiveAuditEntry{
			actor: sess.Principal.ID, role: sess.Principal.Role, resource: r.URL.Path, method: r.Method,
		}) {
			response.JSON(w, http.StatusServiceUnavailable, map[string]any{
				"message": "敏感操作审计不可用，请稍后重试",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

const sensitiveAuditBatch = 32
const sensitiveAuditQueue = 256

type sensitiveAuditEntry struct {
	actor                  int64
	role, resource, method string
	ctx                    context.Context
	done                   chan bool
}

// Coalesce concurrent audit writes while a previous batch is in flight.
// Requests wait outside the SQL pool and cannot reach the protected handler
// until their own audit row is committed. No timer or persistent worker is
// necessary: under light load the first row is submitted immediately.
type sensitiveAuditWriter struct {
	pool    *dbstore.Database
	mu      sync.Mutex
	pending []sensitiveAuditEntry
	running bool
}

func (a *sensitiveAuditWriter) record(ctx context.Context, entry sensitiveAuditEntry) bool {
	if ctx.Err() != nil ||
		!utf8.ValidString(entry.resource) || utf8.RuneCountInString(entry.resource) > 128 ||
		!utf8.ValidString(entry.method) || utf8.RuneCountInString(entry.method) > 8 ||
		!utf8.ValidString(entry.role) || utf8.RuneCountInString(entry.role) > 16 {
		return false
	}
	entry.ctx = ctx
	entry.done = make(chan bool, 1)
	a.mu.Lock()
	if len(a.pending) >= sensitiveAuditQueue {
		a.mu.Unlock()
		return false
	}
	a.pending = append(a.pending, entry)
	if !a.running {
		a.running = true
		go a.flush()
	}
	a.mu.Unlock()
	select {
	case ok := <-entry.done:
		return ok && ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

func (a *sensitiveAuditWriter) flush() {
	var batch [sensitiveAuditBatch]sensitiveAuditEntry
	var args [sensitiveAuditBatch * 4]any
	for {
		a.mu.Lock()
		if len(a.pending) == 0 {
			a.running = false
			a.mu.Unlock()
			return
		}
		n := copy(batch[:], a.pending)
		clear(a.pending[:n])
		a.pending = a.pending[n:]
		a.mu.Unlock()

		values := args[:0]
		for i := range n {
			entry := &batch[i]
			if entry.ctx.Err() != nil {
				entry.done <- false
				entry.done = nil
				continue
			}
			values = append(values, entry.actor, entry.role, entry.resource, entry.method)
		}
		ok := false
		if len(values) != 0 {
			query := "INSERT INTO care_sensitive_access_audit(actor_id,actor_role,resource,method) VALUES " +
				strings.TrimSuffix(strings.Repeat("(?,?,?,?),", len(values)/4), ",")
			// One canceled request must not abort the other rows. Bound the
			// independent commit by the MySQL connection I/O timeout.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err := a.pool.ExecContext(ctx, query, values...)
			cancel()
			ok = err == nil
		}
		for i := range n {
			if batch[i].done != nil {
				batch[i].done <- ok
			}
		}
		clear(batch[:n])
		clear(values)
	}
}
