package observability

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkRegistryHTTP(b *testing.B) {
	// Keep structured logging enabled but exclude disk/network sink latency.
	prior := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(prior)
	r := New()
	h := r.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		req := httptest.NewRequest("GET", "/healthz", nil)
		for pb.Next() {
			h.ServeHTTP(httptest.NewRecorder(), req)
		}
	})
}

func BenchmarkRegistryScrape(b *testing.B) {
	r := New()
	for i := range 100 {
		r.observeHTTP(fmt.Sprintf(`route="route%d",method="GET",status="200"`, i), .125)
	}
	req := httptest.NewRequest("GET", "/metrics", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
}
