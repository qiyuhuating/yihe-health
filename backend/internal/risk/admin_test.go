package risk

import (
	"net/netip"
	"path/filepath"
	"testing"
	"time"
)

func TestManagedBlockAndRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "risk")
	m := New()
	if err := m.Persist(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	r, err := m.AddRule("192.0.2.0/24", "block", "repeated authentication attacks", "admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if m.BeforeLogin("192.0.2.45", "alice").Allowed() {
		t.Fatal("rule did not block")
	}
	if !m.BeforeLogin("198.51.100.1", "alice").Allowed() {
		t.Fatal("unrelated ip blocked")
	}
	m.SuccessfulLogin("198.51.100.1", "13812345678")
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	m2 := New()
	if err := m2.Persist(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m2.Close(); err != nil {
			t.Error(err)
		}
	})
	events, rules := m2.History(10)
	if len(rules) != 1 || len(events) != 2 {
		t.Fatalf("persistence mismatch %v %v", rules, events)
	}
	if events[0].Account != "138****5678" {
		t.Fatalf("unexpected mask %s", events[0].Account)
	}
	if !netip.MustParsePrefix(rules[0].CIDR).Contains(netip.MustParseAddr("192.0.2.45")) {
		t.Fatal("invalid CIDR")
	}
	ok, err := m2.DeleteRule(r.ID, "admin")
	if err != nil || !ok {
		t.Fatalf("delete %v %v", ok, err)
	}
	if !m2.BeforeLogin("192.0.2.45", "alice").Allowed() {
		t.Fatal("block not cleared")
	}
}

func TestRulePrecedenceAndExpiry(t *testing.T) {
	m := New()
	now := time.Now()
	m.now = func() time.Time { return now }
	_, err := m.AddRule("2001:db8::/32", "skip-auto", "trusted office connection", "admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		m.FailedLogin("2001:db8::1", string(rune('a'+i)))
	}
	if !m.BeforeLogin("2001:db8::1", "fresh").Allowed() {
		t.Fatal("skip-auto not working")
	}
	_, err = m.AddRule("2001:db8::1", "block", "manually confirmed abuse", "admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if m.BeforeLogin("2001:db8::1", "fresh").Allowed() {
		t.Fatal("manual block must win")
	}
	now = now.Add(2 * time.Hour)
	if !m.BeforeLogin("2001:db8::1", "fresh").Allowed() {
		t.Fatal("expired rule still active")
	}
}

func TestAuditBatchFlushAndRuleDurability(t *testing.T) {
	dir := t.TempDir()
	m := New()
	if err := m.Persist(dir); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 250; i++ {
		m.FailedLogin("198.51.100.9", "13812345678")
	}
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	copy := New()
	if err := copy.Persist(dir); err != nil {
		t.Fatal(err)
	}
	events, _ := copy.History(200)
	if len(events) != 200 {
		t.Fatalf("got %d events", len(events))
	}
	if err := copy.Close(); err != nil {
		t.Fatal(err)
	}
	rule, err := m.AddRule("203.0.113.0/24", "block", "confirmed abuse", "admin", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fresh := New()
	if err := fresh.Persist(dir); err != nil {
		t.Fatal(err)
	}
	if fresh.BeforeLogin("203.0.113.5", "a").Allowed() {
		t.Fatal("block not durable")
	}
	if rule.ID == "" {
		t.Fatal("rule id missing")
	}
	if err := fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}
