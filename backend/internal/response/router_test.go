package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterContract(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/example", func(w http.ResponseWriter, r *http.Request) { OK(w, nil) })
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{"POST", "/api/v1/example", 200, ""},
		{"GET", "/api/v1/example", 405, "METHOD_NOT_ALLOWED"},
		{"POST", "/api/v1/missing", 404, "NOT_FOUND"},
	} {
		w := httptest.NewRecorder()
		Router(mux).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if tc.code != "" && (body["returnCode"] != "0000" || body["code"] != tc.code) {
			t.Fatal(body)
		}
		if tc.status == 405 && w.Header().Get("Allow") != "POST" {
			t.Fatal(w.Header())
		}
	}
}
