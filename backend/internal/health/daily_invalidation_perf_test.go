package health

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

// Existing dirty cache rows require no further physical update. The owner
// revision lock still guarantees that refresh and invalidation serialize.
func TestSaveBatchOnlyInvalidatesCleanDailyCacheRows(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	a := strings.Repeat("a", 64)
	b := strings.Repeat("b", 64)
	c := strings.Repeat("c", 64)
	for _, tc := range []struct {
		key   string
		owner int
		dirty bool
	}{{a, 1, false}, {b, 1, true}, {c, 2, false}} {
		_, err := p.ExecContext(ctx, `INSERT INTO metric_daily
		(cache_key,old_id,source,source_device,metric,semantics,record_day,offset_minutes,dirty)
		VALUES(?,?,'sim','sim','heart_rate','instant','2026-10-22',0,?)`, tc.key, tc.owner, tc.dirty)
		if err != nil {
			t.Fatal(err)
		}
	}
	sampleTime := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	sample := Sample{
		RecordID: "dirty-cache-regression", Source: "sim", SourceDevice: "sim",
		Metric: "heart_rate", Unit: "count/min", Value: Number(70),
		Start: sampleTime, End: sampleTime, Version: 1,
	}
	// A rollback cannot leak an invalidation or a sample.
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback() // Always release locks before database cleanup, even on Fatal.
	if _, _, err := SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{sample}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	assertDirty := func(key string, want bool) {
		t.Helper()
		var dirty bool
		if err := p.QueryRowContext(ctx, "SELECT dirty FROM metric_daily WHERE cache_key=?", key).Scan(&dirty); err != nil || dirty != want {
			t.Fatalf("cache %s dirty=%v want=%v err=%v", key[:4], dirty, want, err)
		}
	}
	assertDirty(a, false)
	assertDirty(b, true)
	assertDirty(c, false)

	tx, err = p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{sample}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	assertDirty(a, true)
	assertDirty(b, true)
	assertDirty(c, false)
}
