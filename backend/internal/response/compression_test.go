package response

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONCompressionNegotiation(t *testing.T) {
	for _, tc := range []struct {
		path, accept string
		size         int
		compressed   bool
	}{
		{"/api/v1/health/changes", "gzip", 4096, true},
		{"/api/v1/health/statistics", "gzip", 4096, true},
		{"/api/v1/health/statistics", "gzip;q=0", 4096, false},
		{"/api/v1/health/statistics", "gzip", 8, false},
		{"/api/v1/auth/login", "gzip", 4096, false},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, nil)
		req.Header.Set("Accept-Encoding", tc.accept)
		w := httptest.NewRecorder()
		payload := strings.Repeat("x", tc.size)
		CompressJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { OK(w, map[string]any{"data": payload}) })).ServeHTTP(w, req)
		if got := w.Header().Get("Content-Encoding") == "gzip"; got != tc.compressed {
			t.Fatalf("%+v encoding=%v", tc, w.Header())
		}
		var body io.Reader = w.Body
		if tc.compressed {
			z, err := gzip.NewReader(body)
			if err != nil {
				t.Fatal(err)
			}
			defer z.Close()
			body = z
		}
		raw, err := io.ReadAll(body)
		if err != nil || !strings.Contains(string(raw), payload) {
			t.Fatalf("invalid decoded payload: %v", err)
		}
	}
}
