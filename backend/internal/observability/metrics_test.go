package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPLogRecordsRemainStructured(t *testing.T) {
	for _, format := range []string{"json", "text"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			var handler slog.Handler = slog.NewJSONHandler(&output, nil)
			if format == "text" {
				handler = slog.NewTextHandler(&output, nil)
			}
			previous := slog.Default()
			slog.SetDefault(slog.New(handler))
			defer slog.SetDefault(previous)
			r := New()
			const route = "fixture\r\nFORGED=yes\t\"value\""
			r.SetRouteResolver(func(*http.Request) string { return route })
			h := r.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
			req := httptest.NewRequest("GET", "/unmatched?secret=must-not-be-logged", nil)
			h.ServeHTTP(httptest.NewRecorder(), req)
			logged := output.String()
			if strings.Count(logged, "\n") != 1 || strings.Contains(logged, "\r") || strings.Contains(logged, "must-not-be-logged") {
				t.Fatal("HTTP log escaped its structured record or included query data")
			}
			if format == "json" {
				var record map[string]any
				if err := json.Unmarshal(output.Bytes(), &record); err != nil || record["route"] != route || record["msg"] != "http_request" || record["status"] != float64(503) {
					t.Fatal("HTTP JSON log lost its authenticated record boundaries", err)
				}
			} else if !strings.Contains(logged, `\r\nFORGED=yes\t\"value\"`) {
				t.Fatal("HTTP text handler did not quote control characters")
			}
		})
	}
}

func TestBoundedRoutesAndRequestID(t *testing.T) {
	r := New()
	h := r.HTTP(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("X-Request-ID") == "spoof" {
			t.Fatal("untrusted trace accepted")
		}
		w.WriteHeader(503)
	}))
	q := httptest.NewRequest("CUSTOMSECRET", "/not-a-route/13800000001?secret=private", nil)
	q.Header.Set("X-Request-ID", "spoof")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, q)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, q)
	text := out.Body.String()
	if strings.Contains(text, "13800000001") || strings.Contains(text, "private") || strings.Contains(text, "CUSTOMSECRET") || !strings.Contains(text, `route="unknown"`) || !strings.Contains(text, `method="OTHER"`) {
		t.Fatal(text)
	}
	if w.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing trace")
	}
}

func TestSpecialRouteMetricsUseCanonicalLiterals(t *testing.T) {
	for _, path := range []string{"/metrics", "/healthz", "/openapi.json"} {
		t.Run(path, func(t *testing.T) {
			registry := New()
			h := registry.HTTP(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(httptest.NewRecorder(), req)
			result := httptest.NewRecorder()
			registry.ServeHTTP(result, req)
			if !strings.Contains(result.Body.String(), `route="`+path+`",method="GET",status="204"`) {
				t.Fatalf("lost canonical route metric: %s", result.Body.String())
			}
		})
	}
}
