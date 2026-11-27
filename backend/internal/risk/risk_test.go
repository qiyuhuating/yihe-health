package risk

import (
	"fmt"
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

func TestIPv6NetworkFailureBudget(t *testing.T) {
	m := New()
	now := time.Now()
	m.now = func() time.Time { return now }
	for i := range loginNetworkLimit {
		ip := fmt.Sprintf("2001:db8:1:2::%x", i+1)
		if !m.BeforeLogin(ip, "resident").Allowed() {
			t.Fatalf("premature shared cooldown at %d", i)
		}
		d := m.FailedLogin(ip, "resident")
		if d.Allowed() != (i < loginNetworkLimit-1) {
			t.Fatalf("unexpected network budget at %d: %+v", i, d)
		}
	}
	ip := "2001:db8:1:2::ffff"
	if m.BeforeLogin(ip, "other").Allowed() {
		t.Fatal("shared network cooldown missing")
	}
	if !m.BeforeLogin("2001:db8:1:3::1", "resident").Allowed() ||
		!m.BeforeInvite(ip).Allowed() {
		t.Fatal("network penalty leaked to a different prefix or operation")
	}
	m.SuccessfulLogin(ip, "resident")
	if m.BeforeLogin(ip, "resident").Allowed() {
		t.Fatal("successful login erased shared failure evidence")
	}
	now = now.Add(ipCooldown)
	if !m.BeforeLogin(ip, "resident").Allowed() {
		t.Fatal("shared cooldown did not expire")
	}
}

func TestIPv6InviteNetworkBudgetAndIsolation(t *testing.T) {
	m := New()
	now := time.Now()
	m.now = func() time.Time { return now }
	for i := range inviteNetworkLimit {
		ip := fmt.Sprintf("2001:db8:2:1::%x", i+1)
		if !m.BeforeInvite(ip).Allowed() {
			t.Fatalf("premature cooldown at %d", i)
		}
		if d := m.InvalidInvite(ip); d.Allowed() != (i < inviteNetworkLimit-1) {
			t.Fatalf("unexpected invite network decision: %+v", d)
		}
	}
	if m.BeforeInvite("2001:db8:2:1::ffff").Allowed() {
		t.Fatal("invite network cooldown missing")
	}
	if !m.BeforeInvite("2001:db8:2:2::1").Allowed() ||
		!m.BeforeLogin("2001:db8:2:1::ffff", "resident").Allowed() {
		t.Fatal("invite penalty leaked to login or another prefix")
	}
	now = now.Add(inviteCooldown)
	if !m.BeforeInvite("2001:db8:2:1::ffff").Allowed() {
		t.Fatal("invite network cooldown did not expire")
	}
}

func TestNetworkBudgetCanonicalAddresses(t *testing.T) {
	m := New()
	for range accountLimit {
		m.FailedLogin("2001:0db8:0:0:0:0:0:1", "resident")
	}
	if m.BeforeLogin("2001:db8::1", "resident").Allowed() {
		t.Fatal("equivalent IPv6 address did not share exact quota")
	}
	m.SuccessfulLogin("2001:db8::1", "resident")
	if !m.BeforeLogin("2001:0db8::1", "resident").Allowed() {
		t.Fatal("successful login did not reset exact quota")
	}
	for range accountLimit {
		m.FailedLogin("::ffff:192.0.2.1", "resident")
	}
	if m.BeforeLogin("192.0.2.1", "resident").Allowed() {
		t.Fatal("IPv4-mapped address did not share exact quota")
	}
	if !m.BeforeLogin("192.0.2.2", "resident").Allowed() {
		t.Fatal("independent IPv4 address was affected")
	}
}
