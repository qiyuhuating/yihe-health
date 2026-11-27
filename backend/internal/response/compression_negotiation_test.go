package response

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestGzipHeaderNegotiation(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   bool
	}{
		{[]string{"br, gzip;q=0.5, deflate"}, true},
		{[]string{"br", "gzip"}, true},
		{[]string{"gzip", "gzip;q=0"}, false},
		{[]string{"gzip;q=0, gzip"}, false},
		{[]string{"GZIP ; Q = 1"}, true},
		{[]string{"gzip;q=NaN"}, false},
		{[]string{"gzip;q=2"}, false},
		{[]string{"br, *"}, false},
		{nil, false},
	} {
		if got := acceptsGzip(tc.values...); got != tc.want {
			t.Fatalf("%q: got %v", tc.values, got)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/health/changes", nil)
		for _, v := range tc.values {
			req.Header.Add("Accept-Encoding", v)
		}
		w := httptest.NewRecorder()
		CompressJSON(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { OK(w, map[string]any{"data": strings.Repeat("a", 4096)}) })).ServeHTTP(w, req)
		if got := w.Header().Get("Content-Encoding") == "gzip"; got != tc.want {
			t.Fatalf("HTTP %q: got %v", tc.values, got)
		}
	}
}

var gzipResult bool

func BenchmarkGzipNegotiation(b *testing.B) {
	for _, value := range []string{"gzip", "br, gzip;q=0.8, deflate", "br, deflate, zstd", "gzip;q=0"} {
		b.Run(value+"/legacy", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				gzipResult = acceptsGzipLegacy(value)
			}
		})
		b.Run(value+"/scan", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				gzipResult = acceptsGzip(value)
			}
		})
	}
}
func acceptsGzipLegacy(value string) bool {
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(item, ";")
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "gzip") {
			continue
		}
		for _, param := range parts[1:] {
			key, v, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
				q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				if err != nil || !(q > 0 && q <= 1) {
					return false
				}
			}
		}
		return true
	}
	return false
}
