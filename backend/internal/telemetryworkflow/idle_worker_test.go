package telemetryworkflow

import (
	"context"
	"database/sql"
	"slices"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/telemetryrepo"
)

func TestIdleWorkerPreservesDurableOrder(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := t.Context()
	now := time.Now().UTC()
	var processed []int64
	process := func(ctx context.Context, tx *sql.Tx, item telemetryrepo.DurableItem) error {
		processed = append(processed, item.ID)
		return telemetryrepo.MarkDurableDone(ctx, tx, item.ID, now)
	}
	check := func(idle bool, want int) {
		t.Helper()
		if got, err := drainReady(ctx, p, process, idle); err != nil || got != want {
			t.Fatalf("drainReady(idle=%v) = %d, %v; want %d", idle, got, err, want)
		}
	}
	check(false, 0)
	check(true, 0)
	for i, retry := range []time.Time{now.Add(time.Hour), now.Add(-time.Minute)} {
		if _, err := p.ExecContext(ctx, `INSERT INTO telemetry_inbox
 (device_id,elder_id,binding_epoch,request_id,payload,status,created_at,next_retry_at)
 VALUES(1,1,1,?,'fixture','pending',?,?)`, string(rune('a'+i)), now, retry); err != nil {
			t.Fatal(err)
		}
	}
	// A later ready row must not overtake an earlier retry.
	check(true, 0)
	if len(processed) != 0 {
		t.Fatal("later row overtook the oldest pending row")
	}
	if _, err := p.ExecContext(ctx, "UPDATE telemetry_inbox SET next_retry_at=? WHERE request_id='a'", now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	check(true, 2)
	if !slices.Equal(processed, []int64{1, 2}) {
		t.Fatalf("processed out of order: %v", processed)
	}
	check(true, 0)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := drainReady(cancelled, p, process, true); err == nil {
		t.Fatal("probe swallowed cancellation")
	}
}
