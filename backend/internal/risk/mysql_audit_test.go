package risk

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMySQLAuditRequiresDatabase(t *testing.T) {
	m := New()
	if err := m.EnableMySQL(nil); err == nil {
		t.Fatal("expected nil DB to be rejected")
	}
	events, rules, err := m.HistoryMySQL(context.Background(), 10)
	if err != nil || len(events) != 0 || len(rules) != 0 {
		t.Fatalf("unexpected fallback history: %v %v %v", events, rules, err)
	}
}
func TestAuditQueueBounded(t *testing.T) {
	a := &mysqlAudit{queue: make(chan Event, 1)}
	a.push(Event{Type: "failed"})
	a.push(Event{Type: "failed"})
	if got := a.dropped.Load(); got != 1 {
		t.Fatalf("overflow count %d", got)
	}
}

func TestAuditSpoolRecoveryAndIdempotentClear(t *testing.T) {
	a := &mysqlAudit{spoolPath: filepath.Join(t.TempDir(), "risk-audit.jsonl")}
	initial := []Event{{ID: "a", IP: "203.0.113.9", Type: "login_failed"}, {ID: "b", Type: "login_success"}}
	if err := a.persistBacklog(initial); err != nil {
		t.Fatal(err)
	}
	got, err := a.readBacklog()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, initial) {
		t.Fatalf("recovery mismatch: %#v", got)
	}
	if err := a.persistBacklog(nil); err != nil {
		t.Fatal(err)
	}
	got, err = a.readBacklog()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty spool, got %d", len(got))
	}
}
