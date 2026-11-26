package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceAlertDetailRejectsInvalidKindBeforeDatabaseRead(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/device/alertDetail", strings.NewReader(`{"deviceId":"1","kind":"unknown"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	(&Handler{}).deviceAlertDetail(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind: got %d; want 400; body=%s", response.Code, response.Body.String())
	}
}
