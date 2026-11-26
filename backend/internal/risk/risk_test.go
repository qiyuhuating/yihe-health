package risk

import (
	"testing"
	"time"
)

func TestVerifiedFailuresAndIsolation(t *testing.T) {
	m := New()
	now := time.Now()
	m.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		if d := m.FailedLogin("ip-a", "account"); !d.Allowed() {
			t.Fatalf("premature cooldown at %d", i)
		}
	}
	if d := m.FailedLogin("ip-a", "account"); d.RetryAfter != 300 {
		t.Fatalf("missing cooldown: %+v", d)
	}
	if d := m.BeforeLogin("ip-b", "account"); !d.Allowed() {
		t.Fatal("unrelated IP impacted")
	}
	if d := m.BeforeLogin("ip-a", "another-account"); !d.Allowed() {
		t.Fatal("same NAT other account impacted")
	}
	now = now.Add(6 * time.Minute)
	if d := m.BeforeLogin("ip-a", "account"); !d.Allowed() {
		t.Fatal("cooldown did not expire")
	}
}
func TestIPWideLimitAndInvalidInvite(t *testing.T) {
	m := New()
	now := time.Now()
	m.now = func() time.Time { return now }
	for i := 0; i < 30; i++ {
		m.FailedLogin("ip-a", string(rune('a'+i)))
	}
	if d := m.BeforeLogin("ip-a", "new"); d.RetryAfter == 0 {
		t.Fatal("missing IP-wide cooldown")
	}
	if d := m.BeforeLogin("ip-b", "new"); !d.Allowed() {
		t.Fatal("independent IP rejected")
	}
	for i := 0; i < 12; i++ {
		m.InvalidInvite("ip-c")
	}
	if d := m.BeforeInvite("ip-c"); d.RetryAfter == 0 {
		t.Fatal("invite probing not throttled")
	}
	now = now.Add(3 * time.Minute)
	if d := m.BeforeInvite("ip-c"); !d.Allowed() {
		t.Fatal("invite cooldown did not expire")
	}
}
