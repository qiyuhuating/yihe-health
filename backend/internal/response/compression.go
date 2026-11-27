package response

import (
	"compress/gzip"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var jsonGzip = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return w
}}

// Restrict compression to known read-only JSON endpoints without session tokens
// or reflected credentials. No change to cookies, downloads, or streaming.
func CompressJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/health/changes", "/api/v1/health/daily-query", "/api/v1/health/statistics", "/api/v1/vitals/latest", "/api/v1/activity/latest", "/api/v1/operations/snapshot":
		default:
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		if acceptsGzip(r.Header.Values("Accept-Encoding")...) {
			next.ServeHTTP(&jsonCompressionWriter{ResponseWriter: w}, r)
		} else {
			next.ServeHTTP(w, r)
		}
	})
}

func acceptsGzip(values ...string) bool {
	accepted := false
	for _, value := range values {
		for value != "" {
			item, rest, _ := strings.Cut(value, ",")
			value = rest
			name, params, _ := strings.Cut(item, ";")
			if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
				continue
			}
			for params != "" {
				param, rest, _ := strings.Cut(params, ";")
				params = rest
				key, v, ok := strings.Cut(strings.TrimSpace(param), "=")
				if ok && strings.EqualFold(strings.TrimSpace(key), "q") {
					q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
					if err != nil || !(q > 0 && q <= 1) {
						return false
					}
				}
			}
			accepted = true
		}
	}
	return accepted
}

type jsonCompressionWriter struct{ http.ResponseWriter }

func (w *jsonCompressionWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *jsonCompressionWriter) writeCompressed(status int, encoded []byte) bool {
	h := w.Header()
	if len(encoded) < 1024 || status != http.StatusOK || h.Get("Set-Cookie") != "" || h.Get("Content-Encoding") != "" {
		return false
	}
	h.Set("Content-Encoding", "gzip")
	h.Del("Content-Length")
	w.WriteHeader(status)
	z := jsonGzip.Get().(*gzip.Writer)
	z.Reset(w.ResponseWriter)
	_, err := z.Write(encoded)
	if err == nil {
		_, _ = z.Write([]byte{'\n'})
	}
	_ = z.Close()
	z.Reset(io.Discard)
	jsonGzip.Put(z)
	return true
}
