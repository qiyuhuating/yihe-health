package session_test

import (
	"bytes"
	"runtime"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/session"
)

func maintenanceWorkers(t *testing.T) int {
	t.Helper()
	var stacks bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&stacks, 2); err != nil {
		t.Fatal(err)
	}
	return strings.Count(stacks.String(), "created by yihe-care/internal/session.NewStore")
}

// This exact fixture also runs on preview@50b9cc0. Its absent Close API is
// detected so the original retained workers can be measured rather than hidden.
func TestStoreCleanupLifecycleAudit(t *testing.T) {
	const count = 25
	before := maintenanceWorkers(t)
	stores := make([]*session.Store, count)
	for i := range stores {
		stores[i] = session.NewStore(time.Hour)
	}
	closable := true
	for _, store := range stores {
		if closer, ok := any(store).(interface{ Close() }); ok {
			closer.Close()
		} else {
			closable = false
		}
	}
	after := maintenanceWorkers(t)
	deadline := time.Now().Add(5 * time.Second)
	for closable && after != before && time.Now().Before(deadline) {
		runtime.Gosched()
		after = maintenanceWorkers(t)
	}
	t.Logf("stores=%d close_supported=%v maintenance_workers_before=%d after=%d retained=%d", count, closable, before, after, after-before)
	if closable && after != before {
		t.Fatal("closed stores retained maintenance workers")
	}
}
