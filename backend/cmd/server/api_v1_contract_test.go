package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoV1OnlyWhenDatabaseUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	handler := (&databaseService{}).middleware(mux)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/service/status", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "\"primaryEngine\":\"mysql\"") {
		t.Fatalf("native Go API v1 status: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready Go API v1 should return 503, got %d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/unversioned/auth/session", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unversioned API must not exist, got %d", w.Code)
	}
}
