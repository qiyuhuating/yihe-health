package telemetryrepo

import (
	"strings"
	"testing"
)

func TestDefaultProfileInsertUsesNativeMySQL(t *testing.T) {
	if !strings.Contains(watchProfileInsertSQL, "ON DUPLICATE KEY UPDATE device_id=device_id") ||
		strings.Contains(watchProfileInsertSQL, "ON CONFLICT") {
		t.Fatalf("watch_info SQL is not native MySQL: %s", watchProfileInsertSQL)
	}
}
