package risk

import (
	"fmt"
	"net/netip"
	"testing"
	"time"
)

func TestSparseNetworkClassificationMatchesFullWalk(t *testing.T) {
	m := New()
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	prefix := netip.MustParsePrefix
	signals := map[netip.Prefix]NetworkSignal{
		prefix("0.0.0.0/0"):        {Kind: "hosting", Confidence: 80, ObservedAt: now},
		prefix("203.0.113.0/24"):   {Kind: "vpn", Confidence: 95, ObservedAt: now.Add(-31 * 24 * time.Hour)},
		prefix("203.0.113.128/25"): {Kind: "tor", Confidence: 99, ObservedAt: now},
		prefix("2001:db8::/32"):    {Kind: "enterprise_proxy", Confidence: 90, ObservedAt: now},
		prefix("2001:db8::1/128"):  {Kind: "vpn", Confidence: 100, ObservedAt: now.Add(48 * time.Hour)},
	}
	legacy := map[netip.Prefix]struct{}{prefix("203.0.113.0/24"): {}, prefix("2001:db8::1/128"): {}}
	m.publishIntelligence(signals, legacy)
	compare := func(ip string) {
		t.Helper()
		addr, err := netip.ParseAddr(ip)
		want := NetworkClassification{Kind: "unknown"}
		if err == nil {
			addr = addr.Unmap()
			want = NetworkClassification{Kind: "unclassified"}
			for bits := addr.BitLen(); bits >= 0; bits-- {
				p := netip.PrefixFrom(addr, bits).Masked()
				if row, ok := signals[p]; ok && now.Sub(row.ObservedAt) >= -24*time.Hour && now.Sub(row.ObservedAt) <= 30*24*time.Hour {
					want = NetworkClassification{Kind: row.Kind, Confidence: row.Confidence, Provider: row.Provider, Source: row.Source}
					break
				}
				if _, ok := legacy[p]; ok {
					want = NetworkClassification{Kind: "vpn_or_proxy", Confidence: 100, Source: "legacy-curated-cidr"}
					break
				}
			}
		}
		if got := m.classifyNetworkLocked(ip); got != want {
			t.Fatalf("%s: got %+v want %+v", ip, got, want)
		}
	}
	for i := 0; i < 256; i++ {
		compare(fmt.Sprintf("203.0.113.%d", i))
	}
	for _, ip := range []string{"::ffff:203.0.113.1", "2001:db8::1", "2001:db8::2", "2001:db9::2", "198.51.100.8", "not-an-ip"} {
		compare(ip)
	}
	// One feed can refresh independently; keep the other feed and recompute lengths.
	signals = map[netip.Prefix]NetworkSignal{prefix("2001:db9::/32"): {Kind: "tor", Confidence: 99, ObservedAt: now}}
	m.publishIntelligence(signals, nil)
	compare("2001:db9::2")
	compare("2001:db8::1")
	compare("203.0.113.1")
	legacy = map[netip.Prefix]struct{}{}
	m.publishIntelligence(nil, legacy)
	compare("203.0.113.1")
	compare("2001:db8::1")
	compare("2001:db9::2")
}

func TestAuditHistoryRetainsNewestAndCopies(t *testing.T) {
	m := New()
	now := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	for i := 0; i < 2010; i++ {
		m.RecordDenied("198.51.100.8", "test", fmt.Sprint(i))
	}
	events, _ := m.History(200)
	if len(m.events) != 2000 || m.events[0].Detail != "10" || len(events) != 200 || events[0].Detail != "2009" || events[199].Detail != "1810" {
		t.Fatalf("wrong retained history: %d", len(m.events))
	}
	now = now.Add(31 * 24 * time.Hour)
	m.RecordDenied("198.51.100.8", "test", "new")
	recent, _ := m.History(200)
	if len(recent) != 1 || recent[0].Detail != "new" || events[0].Detail != "2009" {
		t.Fatal("expiry or snapshot aliasing")
	}
}

func BenchmarkFileAuditHistory(b *testing.B) {
	m := New()
	m.now = func() time.Time { return time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC) }
	for i := 0; i < 2000; i++ {
		m.RecordDenied("198.51.100.8", "test", "denied")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.RecordDenied("198.51.100.8", "test", "denied")
	}
}
