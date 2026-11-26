package ratelimit

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTrustedProxyBoundary(t *testing.T) {
	l, err := NewWithOptions(Options{TrustedProxyCIDRs: []string{"10.0.0.0/8", "2001:db8:ffff::/48"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	for _, tc := range []struct{ name, peer, xff, forwarded, want string }{
		{"direct forgery", "192.0.2.1:42", "203.0.113.7", "for=203.0.113.8", "192.0.2.1"},
		{"trusted chain", "10.0.0.1:42", "203.0.113.7, 10.1.2.3", "", "203.0.113.7"},
		{"untrusted hop stops spoof", "10.0.0.1:42", "203.0.113.7, 192.0.2.99", "", "192.0.2.99"},
		{"forwarded IPv6", "[2001:db8:ffff::1]:42", "", `for="[2001:db8::7]:123";proto=https`, "2001:db8::7"},
		{"IPv6 canonical", "[2001:0db8:0000::7]:42", "", "", "2001:db8::7"},
		{"IPv4 mapped", "[::ffff:192.0.2.1]:42", "203.0.113.7", "", "192.0.2.1"},
		{"malformed", "10.0.0.1:42", "not-an-address", "", "10.0.0.1"},
		{"obfuscated forwarded", "10.0.0.1:42", "203.0.113.7", "for=_hidden", "10.0.0.1"},
		{"duplicate for", "10.0.0.1:42", "", "for=203.0.113.7;for=192.0.2.1", "10.0.0.1"},
		{"oversized", "10.0.0.1:42", strings.Repeat("x", 4097), "", "10.0.0.1"},
		{"long chain", "10.0.0.1:42", strings.Repeat("192.0.2.1,", 33), "", "10.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
			r.RemoteAddr = tc.peer
			if tc.xff != "" {
				r.Header.Set("X-Forwarded-For", tc.xff)
			}
			if tc.forwarded != "" {
				r.Header.Set("Forwarded", tc.forwarded)
			}
			if got := l.clientIP(r); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestHighCardinalityHardCaps256Clients(t *testing.T) {
	l, err := NewWithOptions(Options{MaxKeys: 4096, MaxSlots: 8192})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range 256 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := next.Add(1)
				if i > 100000 {
					return
				}
				r := httptest.NewRequest(http.MethodPost, "/api/v1/setup", nil)
				if i%2 == 0 {
					r.RemoteAddr = fmt.Sprintf("[2001:db8::%x]:42", i)
				} else {
					r.RemoteAddr = fmt.Sprintf("198.%d.%d.%d:42", (i>>16)&255, (i>>8)&255, i&255)
				}
				l.Allow(r)
			}
		}()
	}
	wg.Wait()
	var keys, slots int
	var rejected uint64
	for i := range l.shards {
		s := &l.shards[i]
		if len(s.hits) > s.maxKeys || s.slots > s.maxSlots {
			t.Fatal("shard exceeded hard cap")
		}
		keys += len(s.hits)
		slots += s.slots
		rejected += s.capacityDenied
	}
	if keys > 4096 || slots > 8192 || rejected == 0 {
		t.Fatalf("keys=%d slots=%d denied=%d", keys, slots, rejected)
	}
	var metrics bytes.Buffer
	l.WriteMetrics(&metrics)
	if !strings.Contains(metrics.String(), "yihe_ratelimit_key_cap 4096") {
		t.Fatal(metrics.String())
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("MICROBENCHMARK cardinality=100000 clients=256 keys=%d slots=%d rejected=%d retained_heap_before=%d retained_heap_after=%d (whole-process RSS is not this counter)", keys, slots, rejected, before.HeapAlloc, after.HeapAlloc)
	// Expired records return both budgets; active quotas are not evicted.
	l.sweep(time.Now().Add(window + time.Second))
	for i := range l.shards {
		if len(l.shards[i].hits) != 0 || l.shards[i].slots != 0 {
			t.Fatal("expired memory budget retained")
		}
	}
}

func TestSlotBudgetAndQuotaCannotBeResetByEviction(t *testing.T) {
	l, err := NewWithOptions(Options{MaxKeys: 32, MaxSlots: 32})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/setup", nil)
	r.RemoteAddr = "192.0.2.1:42"
	if !l.Allow(r) || l.Allow(r) {
		t.Fatal("timestamp budget not enforced")
	}
	var other string
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("192.0.%d.%d", i>>8, i&255)
		if l.shard(candidate) == l.shard("192.0.2.1") {
			other = candidate
			break
		}
	}
	r.RemoteAddr = other + ":42"
	if other == "" || l.Allow(r) {
		t.Fatal("active quota was evicted")
	}
	l.sweep(time.Now().Add(window + time.Second))
	if !l.Allow(r) {
		t.Fatal("expired quota was not reclaimed")
	}
}

func TestNATHardwareQuotaAndProxyForgery(t *testing.T) {
	l := New()
	t.Cleanup(l.Close)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/device/report", nil)
	r.RemoteAddr = "192.0.2.77:42"
	for i := 0; i < 1800; i++ {
		if !l.Allow(r) {
			t.Fatalf("NAT quota denied at %d", i)
		}
	}
	r.Header.Set("X-Forwarded-For", "203.0.113.77")
	if l.Allow(r) {
		t.Fatal("forged XFF reset NAT quota")
	}
}

func TestInvalidLimiterConfiguration(t *testing.T) {
	for _, o := range []Options{{MaxKeys: -1}, {MaxKeys: 32, MaxSlots: 31}, {TrustedProxyCIDRs: []string{"not-a-cidr"}}} {
		if _, err := NewWithOptions(o); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
