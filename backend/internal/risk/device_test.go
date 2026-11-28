package risk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIdentifyDevice(t *testing.T) {
	cases := []struct{ ua, hint, typ, os, browser, model string }{
		{"Mozilla/5.0 (Linux; Android 14; Pixel 8 Build/UP1A.231005.007) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120 Mobile Safari/537.36", "", "mobile", "Android", "Chrome", "Pixel 8"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17 Safari/604.1", "", "mobile", "iOS", "Safari", ""},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120 Safari/537.36 Edg/120", "", "desktop", "Windows", "Edge", ""},
		{"Mozilla/5.0 (Linux; Android 15; K) AppleWebKit/537.36 Chrome/120 Mobile Safari/537.36", `"SM-S928B"`, "mobile", "Android", "Chrome", "SM-S928B"},
		{"", "", "unknown", "Unknown", "Unknown", ""},
	}
	for _, c := range cases {
		d := IdentifyDevice(c.ua, c.hint)
		if d.Type != c.typ || d.OS != c.os || d.Browser != c.browser || d.Model != c.model {
			t.Errorf("got %+v, want %s %s %s %s", d, c.typ, c.os, c.browser, c.model)
		}
	}
}
func TestEventRetainsDevice(t *testing.T) {
	m := New()
	m.FailedLogin("192.0.2.7", "13812345678", "Mozilla/5.0 (Linux; Android 14; Pixel 8 Build/UP1A) Chrome/120 Mobile")
	history, _ := m.History(1)
	if len(history) != 1 || history[0].Device.Model != "Pixel 8" || history[0].Account == "13812345678" {
		t.Fatalf("unexpected event: %+v", history)
	}
}

func TestDeviceAuditReportsMissingAndInvalidWithoutTrust(t *testing.T) {
	for _, tc := range []struct{ hint, source string }{
		{"", "not-provided"},
		{"   ", "not-provided"},
		{"bad", "invalid-format"},
		{"invalid<script>", "invalid-format"},
		{strings.Repeat("a", 129), "invalid-format"},
		{"appScoped_123456789012", "app-reported"},
	} {
		d := IdentifyDevice("", "", tc.hint)
		if d.HardwareSource != tc.source {
			t.Fatalf("status=%q want %q", d.HardwareSource, tc.source)
		}
		if tc.source != "app-reported" && d.HardwareID != "" {
			t.Fatal("invalid or missing identifier retained")
		}
	}
	m := New()
	m.FailedLogin("192.0.2.1", "13812345678", "")
	events, _ := m.History(1)
	if len(events) != 1 || events[0].Device.HardwareSource != "not-provided" {
		t.Fatal("missing metadata status not recorded")
	}
}

func TestDeviceAuditTextBoundedAndValid(t *testing.T) {
	ua := "Windows\x00\n\u202e" + strings.Repeat("界", 200)
	d := IdentifyDevice(ua)
	if !utf8.ValidString(d.UserAgent) || len(d.UserAgent) > 512 ||
		strings.ContainsAny(d.UserAgent, "\x00\n\u202e") {
		t.Fatal("unsafe audit text")
	}
	if got := cleanModel("Model\u202e123"); got != "" {
		t.Fatal("invisible formatting accepted in model")
	}
}
