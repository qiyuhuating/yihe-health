package ratelimit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The exact same fixture runs against baseline and candidate. The full-window
// denial path includes IP parsing, quota lookup, clock and locking, but no HTTP
// response encoding. It is not a benchmark of accepted requests or an SLA.
func BenchmarkLimiterSaturated(b *testing.B) {
	for _, clients := range []int{1, 256} {
		b.Run(fmt.Sprintf("clients_%d", clients), func(b *testing.B) {
			l := New()
			if closer, ok := any(l).(interface{ Close() }); ok {
				b.Cleanup(closer.Close)
			}
			requests := make([]*http.Request, clients)
			for i := range requests {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/device/login", nil)
				r.RemoteAddr = fmt.Sprintf("192.0.2.%d:12345", i)
				requests[i] = r
				for range 60 {
					if !l.Allow(r) {
						b.Fatal("fixture quota")
					}
				}
			}
			var sequence atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := int(sequence.Add(1)-1) % clients
				for pb.Next() {
					if l.Allow(requests[i]) {
						b.Error("full window accepted request")
						return
					}
					i = (i + 1) % clients
				}
			})
		})
	}
}
