package risk

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifiedFeedsAndBlockThresholds(t *testing.T) {
	m := New()
	m.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }
	data := "cidr,kind,confidence,observedAt,provider,source\n" +
		"203.0.113.0/24,vpn,98,2026-11-05,VPNCo,verified-feed\n" +
		"198.51.100.0/24,hosting,99,2026-11-05,CloudCo,asn-feed\n" +
		"192.0.2.1/32,residential_proxy,60,2026-11-05,ResNet,provider-feed\n" +
		"192.0.2.2/32,residential_proxy,97,2026-11-05,ResNet,provider-feed\n" +
		"2001:db8::/32,tor,99,2026-11-05,Tor,exit-list\n" +
		"192.0.2.3/32,open_proxy,99,2026-08-01,Proxy,stale-feed\n"
	file := filepath.Join(t.TempDir(), "feed.csv")
	if err := os.WriteFile(file, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadNetworkSignals(file); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"203.0.113.4", "192.0.2.2", "2001:db8::1"} {
		if m.BeforeLogin(ip, "account").Allowed() {
			t.Fatalf("expected block %s", ip)
		}
	}
	for _, ip := range []string{"198.51.100.8", "192.0.2.1", "192.0.2.3"} {
		if !m.BeforeLogin(ip, "account").Allowed() {
			t.Fatalf("unexpected block %s", ip)
		}
	}
	m.RecordDenied("203.0.113.4", "123456789", "proxy_detected")
	events, _ := m.History(1)
	if len(events) != 1 || events[0].Network != "vpn" || events[0].NetworkProvider != "VPNCo" || events[0].NetworkConfidence != 98 {
		t.Fatalf("missing provenance %+v", events)
	}
}
func TestFeedUpdateAtomic(t *testing.T) {
	m := New()
	m.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }
	file := filepath.Join(t.TempDir(), "feed.csv")
	_ = os.WriteFile(file, []byte("203.0.113.1/32,vpn,99,2026-11-05,x,source\n"), 0600)
	if err := m.LoadNetworkSignals(file); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(file, []byte("invalid,vpn,99,2026-11-05,x,source\n"), 0600)
	if err := m.LoadNetworkSignals(file); err == nil {
		t.Fatal("invalid feed accepted")
	}
	if m.BeforeLogin("203.0.113.1", "x").Allowed() {
		t.Fatal("old known good feed lost")
	}
}
