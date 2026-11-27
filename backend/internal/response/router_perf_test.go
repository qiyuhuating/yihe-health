package response

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkRouter(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/example/{id}", func(w http.ResponseWriter, r *http.Request) { OK(w, nil) })
	h := Router(mux)
	for _, tc := range []struct{ name, method, path string }{
		{"matched", "POST", "/api/v1/example/1"},
		{"method", "GET", "/api/v1/example/1"},
		{"missing", "POST", "/api/v1/missing"},
	} {
		b.Run(tc.name, func(b *testing.B) {
			req := httptest.NewRequest(tc.method, tc.path+"?a=1&b=2", nil)
			req.Header.Set("Cookie", "session=synthetic")
			req.Header.Set("Accept", "application/json")
			b.ReportAllocs()
			for range b.N {
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}
