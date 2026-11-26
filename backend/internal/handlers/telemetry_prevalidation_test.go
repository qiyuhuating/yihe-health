package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// These cases use a nil DB deliberately: malformed hardware payloads must
// be rejected without acquiring a transaction or requiring either database.
func TestDeviceReportInvalidPayloadNeverOpensDatabase(t *testing.T) {
	cases := []struct {
		name  string
		field string
		value any
	}{
		{"heart rate", "hr", 251},
		{"blood pressure", "bp", 301},
		{"steps", "step", -1},
		{"sleep", "sleep", 25},
		{"sleep state", "sleepState", "2"},
		{"battery", "battery", 101},
		{"unpaired coordinates", "lng", "120.5"},
		{"invalid timestamp", "beTime", "2026-02-31 13:00:00"},
		{"future timestamp", "beTime", "2099-01-01 00:00:00"},
		{"long app version", "appVersion", "this-version-name-is-far-too-long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := map[string]any{
				"deviceNo": "WATCH001",
				"secret":   "existing-device-secret",
				tc.field:   tc.value,
			}
			response := callAPI((&Handler{}).deviceReport, payload)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("HTTP %d, want 400: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestSOSInvalidRequestIDNeverOpensDatabase(t *testing.T) {
	for _, requestID := range []string{"invalid request ID", "bad/identifier", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789too-long"} {
		response := callAPI((&Handler{}).deviceSos, map[string]any{
			"deviceNo": "WATCH001", "secret": "existing-device-secret",
			"requestId": requestID, "bindingVersion": 1,
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("requestID %q: HTTP %d, want 400: %s", requestID, response.Code, response.Body.String())
		}
	}
}

func TestParseDeviceTelemetryRetainsLegacyFields(t *testing.T) {
	now := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	w := httptest.NewRecorder()
	parsed, ok := parseDeviceTelemetryPayload(w, map[string]any{
		"beTime": "2026-10-27 08:09:10",
		"hr":     "0", "bp": "120", "step": "0", "sleep": "8",
		"sleepState": "1", "source": "gps", "battery": "85",
		"lng": "105.5", "lat": "30.5", "alarm": "fall",
	}, now)
	if !ok || parsed.hr == nil || *parsed.hr != 0 || parsed.bp == nil || *parsed.bp != 120 ||
		parsed.step == nil || *parsed.step != 0 || parsed.sleep == nil || *parsed.sleep != 8 ||
		parsed.sleepState == nil || *parsed.sleepState != "1" || parsed.battery == nil ||
		*parsed.battery != 85 || parsed.alarm != "fall" || parsed.sourceStr != "gps" ||
		parsed.beTime.Format("2006-01-02 15:04:05") != "2026-10-27 08:09:10" ||
		parsed.day.Format("2006-01-02") != "2026-10-27" {
		t.Fatalf("legacy device values changed: %+v, ok=%v, body=%s", parsed, ok, w.Body.String())
	}
	w = httptest.NewRecorder()
	fallback, ok := parseDeviceTelemetryPayload(w, map[string]any{}, now)
	if !ok || !fallback.beTime.Equal(now) || fallback.sourceStr != "gps" {
		t.Fatalf("default time/source changed: %+v, ok=%v", fallback, ok)
	}
}

func TestDeviceReportUnknownAlarmNeverOpensDatabase(t *testing.T) {
	for _, alarm := range []any{"sos", "falling", "2", true, map[string]any{"type": "fall"}} {
		response := callAPI((&Handler{}).deviceReport, map[string]any{
			"deviceNo": "WATCH001", "secret": "existing-device-secret",
			"alarm": alarm,
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unrecognized alarm %v returned HTTP %d: %s", alarm, response.Code, response.Body.String())
		}
	}
}

func TestDeviceReportInvalidBindingVersionNeverOpensDatabase(t *testing.T) {
	for _, version := range []any{nil, -1, 0, 1.5, "unknown", true} {
		response := callAPI((&Handler{}).deviceReport, map[string]any{
			"deviceNo": "WATCH001", "secret": "existing-device-secret",
			"bindingVersion": version, "hr": 75,
		})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid bindingVersion %v returned HTTP %d: %s", version, response.Code, response.Body.String())
		}
	}
}
