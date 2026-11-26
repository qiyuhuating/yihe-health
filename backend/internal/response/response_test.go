package response

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		fields     map[string]any
		wantStatus int
		wantCode   string
	}{
		{"query", 200, map[string]any{"items": []any{}}, 200, ""},
		{"validation", 400, map[string]any{"message": "名称不能为空"}, 400, "INVALID_REQUEST"},
		{"auth", 401, map[string]any{"code": "INVALID_CREDENTIALS", "message": "凭据错误"}, 401, "INVALID_CREDENTIALS"},
		{"business rejection", 200, map[string]any{"returnCode": "0000"}, 400, "INVALID_REQUEST"},
		{"internal", 500, map[string]any{"reference": "request-1"}, 500, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc.fields)
			w := httptest.NewRecorder()
			JSON(w, tc.status, tc.fields)
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			wantReturn := "1111"
			if tc.wantStatus >= 400 {
				wantReturn = "0000"
			}
			if w.Code != tc.wantStatus || body["returnCode"] != wantReturn {
				t.Fatalf("%d %v", w.Code, body)
			}
			if tc.wantCode != "" && (body["code"] != tc.wantCode || body["message"] == nil || body["message"] == "") {
				t.Fatal(body)
			}
			after, _ := json.Marshal(tc.fields)
			if string(before) != string(after) {
				t.Fatal("caller data mutated")
			}
			if tc.fields["reference"] != nil && body["reference"] != tc.fields["reference"] {
				t.Fatal("reference lost")
			}
		})
	}
}

func TestJSONSerializationFailureReturnsSafe500(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, 200, map[string]any{"secret": "private", "unencodable": make(chan int)})
	var got BaseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 500 || got.ReturnCode != CodeFail || got.Code != "INTERNAL_ERROR" {
		t.Fatalf("invalid encoder error contract: %d %+v", w.Code, got)
	}
	if _, ok := w.Header()["Content-Type"]; !ok {
		t.Fatal("missing JSON content type")
	}
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("encoded data escaped on error")
	}
}
