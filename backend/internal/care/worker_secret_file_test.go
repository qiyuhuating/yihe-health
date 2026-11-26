package care

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNotificationConfiguredUsesTokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gateway-token")
	if err := os.WriteFile(path, []byte("file-backed-notification-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CARE_NOTIFY_WEBHOOK_URL", "https://gateway.example.test/notify")
	t.Setenv("CARE_NOTIFY_WEBHOOK_TOKEN", "")
	t.Setenv("CARE_NOTIFY_WEBHOOK_TOKEN_FILE", path)
	if !NotificationConfigured() {
		t.Fatal("valid HTTPS gateway and file-backed token should be configured")
	}
	t.Setenv("CARE_NOTIFY_WEBHOOK_TOKEN_FILE", path+".missing")
	if NotificationConfigured() {
		t.Fatal("missing secret file must fail closed")
	}
}
