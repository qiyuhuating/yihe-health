package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// apiFailure verifies the error contract while leaving success assertions strict.
func apiFailure(t *testing.T, status int, handler http.HandlerFunc, body map[string]any) map[string]any {
	t.Helper()
	w := callAPI(handler, body)
	out := architectureBody(t, w)
	if w.Code != status || out["returnCode"] != "0000" || out["code"] == "" || out["code"] == nil || out["message"] == "" || out["message"] == nil {
		t.Fatalf("want HTTP %d with failure envelope; got %d %s", status, w.Code, w.Body.String())
	}
	return out
}

func TestMissingNoticeReturnsNotFound(t *testing.T) {
	h := testHandler(t, testDatabase(t))
	out := apiFailure(t, 404, h.getNotice, map[string]any{"textId": 999999})
	if out["code"] != "NOT_FOUND" {
		t.Fatal(out)
	}
}

func TestImageFilenameArrayRejectsInvalidTypes(t *testing.T) {
	for _, value := range []any{"a.jpg", 3, []any{true}, []any{map[string]any{"name": "a.jpg"}}} {
		w := httptest.NewRecorder()
		if _, ok := picNameList(w, map[string]any{"fileListName": value}); ok || w.Code != 400 {
			t.Fatalf("accepted invalid image list %v: %d", value, w.Code)
		}
	}
	for _, body := range []map[string]any{{}, {"fileListName": []any{}}, {"fileListName": []any{"a.jpg"}}} {
		if _, ok := picNameList(httptest.NewRecorder(), body); !ok {
			t.Fatal(body)
		}
	}
}
