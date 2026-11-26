package risk

import "testing"

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
