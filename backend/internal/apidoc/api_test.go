package apidoc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContractPublishesNativeGoRoutes(t *testing.T) {
	w := httptest.NewRecorder()
	ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "/api/v1/auth/login") {
		t.Fatalf("missing Go v1 contract: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "YIHE_SESSION") {
		t.Fatal("missing native Go session cookie definition")
	}
}
