package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteStructPreservesFlatContractAndStatus(t *testing.T) {
	type reply struct {
		Data string `json:"data"`
		BaseResponse
	}
	w := httptest.NewRecorder()
	WriteStruct(w, http.StatusAccepted, reply{Data: "ok", BaseResponse: BaseResponse{ReturnCode: CodeOK}})
	if w.Code != http.StatusAccepted || w.Body.String() != "{\"data\":\"ok\",\"returnCode\":\"1111\"}\n" {
		t.Fatalf("flat JSON contract changed: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWriteErrorPreservesDomainCode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"unauthorized", Unauthorized("please login", nil), 401, "AUTH_REQUIRED"},
		{"conflict", &AppError{HTTPStatus: 409, Code: "HANDOVER_PENDING", Message: "pending"}, 409, "HANDOVER_PENDING"},
		{"untyped", errors.New("private SQL details"), 500, "INTERNAL_ERROR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, tc.err)
			var got struct {
				ReturnCode string `json:"returnCode"`
				Code       string `json:"code"`
				Message    string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || got.ReturnCode != CodeFail || got.Code != tc.code || got.Message == "" {
				t.Fatalf("unexpected error contract: %d %+v", w.Code, got)
			}
		})
	}
}

func TestWriteOKRetainsZeroValues(t *testing.T) {
	for _, value := range []any{false, 0, ""} {
		w := httptest.NewRecorder()
		WriteOK(w, value)
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || got["returnCode"] != CodeOK {
			t.Fatalf("unexpected success: %d %v", w.Code, got)
		}
		if _, ok := got["data"]; !ok {
			t.Fatalf("zero payload omitted: %v", got)
		}
	}
}

func TestInternalErrorDoesNotExposeCauseOrMessage(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, Internal("SQL password secret", errors.New("database secret")))
	var got BaseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 500 || got.ReturnCode != CodeFail || got.Code != "INTERNAL_ERROR" || got.Message != internalServerMessage() {
		t.Fatalf("unsafe internal response: %d %+v", w.Code, got)
	}
}

func TestWriteErrorKeepsPublicReference(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, &AppError{HTTPStatus: 409, Code: "CONFLICT", Message: "stale", Reference: "request-1"})
	var got BaseResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || got.Reference != "request-1" {
		t.Fatalf("missing reference: %+v", got)
	}
}

func internalServerMessage() string {
	_, msg := errorDefaults(http.StatusInternalServerError)
	return msg
}
