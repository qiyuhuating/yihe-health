package securityaudit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func queuedAudit(t *testing.T) (*db.Database, string) {
	t.Helper()
	store := dbtest.MySQL(t)
	ctx := context.Background()
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	id, err := Append(ctx, tx, Entry{Actor: Actor{User: "ops", UID: "os-id", Host: "host"}, AdminID: 1, Reason: "INC-test",
		Outcome: "granted", Decision: "granted", Before: Snapshot{Exists: true, Role: "1", Status: "active", Duty: "unassigned"},
		After: Snapshot{Exists: true, Role: "1", Status: "active", Duty: "supervisor"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return store, id
}

func TestSecurityAlertReleasesDatabaseDuringHTTP(t *testing.T) {
	store, id := queuedAudit(t)
	store.SetMaxOpenConns(1)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Priority string `json:"priority"`
			Entry
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Priority != "critical" || payload.ID != id || payload.Actor.User != "ops" || payload.Before.Duty != "unassigned" || payload.After.Duty != "supervisor" {
			t.Error("security alert payload lost evidence", err)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("Idempotency-Key") != "security-"+id {
			t.Error("security alert authorization or idempotency missing")
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	defer close(release)
	w := Worker{DB: store, URL: server.URL, Token: "test-token", Client: server.Client()}
	done := make(chan error, 1)
	go func() { _, err := w.Tick(context.Background()); done <- err }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("alert did not reach gateway")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role) VALUES(1,'unblocked','1')"); err != nil {
		t.Fatalf("HTTP delivery held the only MySQL connection: %v", err)
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var status string
	if err := store.QueryRowContext(ctx, "SELECT status FROM security_alert_outbox WHERE audit_id=?", id).Scan(&status); err != nil || status != "delivered" {
		t.Fatalf("delivery not acknowledged: %s %v", status, err)
	}
	if processed, err := w.Tick(context.Background()); err != nil || processed {
		t.Fatalf("delivered alert was replayed: %t %v", processed, err)
	}
}

func TestSecurityAlertRetryAndLeaseFencing(t *testing.T) {
	store, id := queuedAudit(t)
	ctx := context.Background()
	w := Worker{DB: store}
	_, first, err := w.claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, "UPDATE security_alert_outbox SET available_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE audit_id=?", id); err != nil {
		t.Fatal(err)
	}
	_, second, err := w.claim(ctx)
	if err != nil || first == "" || second == "" || first == second {
		t.Fatalf("lease was not reclaimed: %v", err)
	}
	if err := w.finish(ctx, id, first, true); err != nil {
		t.Fatal(err)
	}
	var status, token string
	if err := store.QueryRowContext(ctx, "SELECT status,lease_token FROM security_alert_outbox WHERE audit_id=?", id).Scan(&status, &token); err != nil || status != "sending" || token != second {
		t.Fatalf("stale worker overwrote new claim: %s %v", status, err)
	}
	if err := w.finish(ctx, id, second, false); err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Bool
	keys := make(chan string, 2)
	server := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		keys <- r.Header.Get("Idempotency-Key")
		if !accepted.Load() {
			rw.WriteHeader(503)
		}
	}))
	defer server.Close()
	w.URL, w.Token, w.Client = server.URL, "test-token", server.Client()
	for _, success := range []bool{false, true} {
		accepted.Store(success)
		if _, err := store.ExecContext(ctx, "UPDATE security_alert_outbox SET available_at=UTC_TIMESTAMP(6) WHERE audit_id=?", id); err != nil {
			t.Fatal(err)
		}
		processed, err := w.Tick(ctx)
		if !processed || (err == nil) != success {
			t.Fatalf("retry result: processed=%t success=%t err=%v", processed, success, err)
		}
		if key := <-keys; key != "security-"+id {
			t.Fatalf("retry changed idempotency key: %s", key)
		}
	}
	if err := store.QueryRowContext(ctx, "SELECT status FROM security_alert_outbox WHERE audit_id=?", id).Scan(&status); err != nil || status != "delivered" {
		t.Fatalf("retry not delivered: %s %v", status, err)
	}
}

func TestSecurityAlertDoesNotFollowRedirects(t *testing.T) {
	store, id := queuedAudit(t)
	var forwarded atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/forwarded" {
			forwarded.Add(1)
			return
		}
		http.Redirect(w, r, "/forwarded", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	w := Worker{DB: store, URL: server.URL, Token: "test-token", Client: server.Client()}
	if processed, err := w.Tick(context.Background()); !processed || err == nil {
		t.Fatalf("redirect accepted: %t %v", processed, err)
	}
	var status string
	if err := store.QueryRowContext(context.Background(), "SELECT status FROM security_alert_outbox WHERE audit_id=?", id).Scan(&status); err != nil || status != "pending" || forwarded.Load() != 0 {
		t.Fatalf("redirect leaked credentials or lost alert: %s %v", status, err)
	}
}
