package telemetryrepo

import (
	"strings"
	"testing"
)

func TestDayUpsertUsesNativeMySQL(t *testing.T) {
	if !strings.Contains(watchDayUpsertSQL, "ON DUPLICATE KEY UPDATE") ||
		!strings.Contains(watchDayUpsertSQL, "new.sleep") ||
		strings.Contains(watchDayUpsertSQL, "EXCLUDED.") ||
		strings.Contains(watchDayUpsertSQL, "ON CONFLICT") {
		t.Fatalf("watch-day upsert is not native MySQL: %s", watchDayUpsertSQL)
	}
}
