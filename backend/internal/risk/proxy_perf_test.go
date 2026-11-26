package risk

import (
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentReloadAndLookups(t *testing.T) {
	m := New()
	file := filepath.Join(t.TempDir(), "feed.csv")
	data := []byte("203.0.113.0/24,vpn,99,2026-11-05,x,feed\n")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }
	if err := m.LoadNetworkSignals(file); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				m.BeforeLogin("203.0.113.9", "x")
				m.BeforeLogin("198.51.100.9", "x")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 100; j++ {
			if err := m.LoadNetworkSignals(file); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
}

func BenchmarkNetworkClassification(b *testing.B) {
	m := New()
	m.now = func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }
	rows := make(map[netip.Prefix]NetworkSignal, 100000)
	for i := 0; i < 100000; i++ {
		addr := netip.AddrFrom4([4]byte{byte(i >> 16), byte(i >> 8), byte(i), 0})
		prefix := netip.PrefixFrom(addr, 24)
		rows[prefix] = NetworkSignal{Prefix: prefix, Kind: "vpn", Confidence: 90, ObservedAt: m.now()}
	}
	m.publishIntelligence(rows, nil)
	ips := []string{"198.51.100.8", "203.0.113.8"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.classifyNetworkLocked(ips[i%2])
	}
}
