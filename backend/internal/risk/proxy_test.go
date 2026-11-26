package risk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProxyFeedAndPrecedence(t *testing.T) {
	m := New()
	path := filepath.Join(t.TempDir(), "proxies.txt")
	if err := os.WriteFile(path, []byte("203.0.113.0/24\n2001:db8::/32\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.LoadProxyCIDRs(path); err != nil {
		t.Fatal(err)
	}
	if m.BeforeLogin("203.0.113.7", "test").Allowed() {
		t.Fatal("proxy not blocked")
	}
	if m.BeforeInvite("2001:db8::a").Allowed() {
		t.Fatal("ipv6 proxy not blocked")
	}
	if !m.BeforeLogin("198.51.100.3", "test").Allowed() {
		t.Fatal("unrelated ip blocked")
	}
	m.RecordDenied("203.0.113.7", "foo", "proxy_detected")
	events, _ := m.History(1)
	if len(events) != 1 || events[0].Network != "vpn_or_proxy" {
		t.Fatalf("missing classifier: %+v", events)
	}
}
func TestReportedHardwareID(t *testing.T) {
	d := IdentifyDevice("Mozilla/5.0 (Linux; Android 14; Pixel 8 Build/UP1A)", "", "appScoped_123456789012")
	if d.HardwareID != "appScoped_123456789012" || d.HardwareSource != "app-reported" {
		t.Fatalf("unexpected: %+v", d)
	}
	d = IdentifyDevice("", "", "invalid<script>")
	if d.HardwareID != "" {
		t.Fatal("invalid hardware id accepted")
	}
}
