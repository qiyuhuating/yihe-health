package health

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
)

func TestTailRangeReservationKeepsCommittedCursorAndBypassesGlobalLock(t *testing.T) {
	p := dbtest.Fixture(t)
	ctx := t.Context()
	at := time.Now().UTC().Truncate(time.Microsecond)
	sample := Sample{RecordID: "one", Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: Number(70), Start: at, End: at, Version: 1}
	write := func(ctx context.Context, owner int64, samples []Sample, commit bool) int64 {
		t.Helper()
		tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		cursor, _, err := SaveBatch(ctx, tx, Owner{OldID: owner}, samples)
		if err != nil {
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		return cursor
	}
	if got := write(ctx, 1, []Sample{sample}, true); got != 1 {
		t.Fatal(got)
	}
	if got := write(ctx, 2, []Sample{sample}, true); got != ownerRevisionRange+1 {
		t.Fatal(got)
	}
	if _, err := p.ExecContext(ctx, "UPDATE health_owner_clock SET revision=range_end-201 WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	a, b := sample, sample
	a.RecordID, b.RecordID = "two", "three"
	want := ownerRevisionRange - 199
	check := func(revision, start, end, high int64) {
		t.Helper()
		var r, s, e, h int64
		if err := p.QueryRowContext(ctx, "SELECT revision,range_start,range_end FROM health_owner_clock WHERE old_id=1").Scan(&r, &s, &e); err != nil || r != revision || s != start || e != end {
			t.Fatal("owner clock", r, s, e, err)
		}
		if err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&h); err != nil || h != high {
			t.Fatal("global clock", h, err)
		}
	}
	if got := write(ctx, 1, []Sample{a, b}, false); got != want {
		t.Fatal(got)
	}
	check(want-2, 0, ownerRevisionRange, 2*ownerRevisionRange)
	if got := write(ctx, 1, []Sample{a, b}, true); got != want {
		t.Fatal(got)
	}
	check(want, 2*ownerRevisionRange, 3*ownerRevisionRange, 3*ownerRevisionRange)
	// A different transaction holds the global allocator; the prefetched owner
	// can nevertheless commit its next real event in the disjoint range.
	block, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer block.Rollback()
	var locked int64
	if err = block.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1 FOR UPDATE").Scan(&locked); err != nil {
		t.Fatal(err)
	}
	c := sample
	c.RecordID = "four"
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if got := write(bounded, 1, []Sample{c}, true); got != 2*ownerRevisionRange+1 {
		t.Fatal(got)
	}
	if err = block.Rollback(); err != nil {
		t.Fatal(err)
	}
	check(2*ownerRevisionRange+1, 2*ownerRevisionRange, 3*ownerRevisionRange, 3*ownerRevisionRange)

	// Rolling back the schema with an unconsumed future range must discard it.
	if _, err = p.ExecContext(ctx, "UPDATE health_owner_clock SET revision=range_end-201 WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	a.RecordID, b.RecordID = "five", "six"
	want = 3*ownerRevisionRange - 199
	if got := write(ctx, 1, []Sample{a, b}, true); got != want {
		t.Fatal(got)
	}
	check(want, 3*ownerRevisionRange, 4*ownerRevisionRange, 4*ownerRevisionRange)
	rollback, err := os.ReadFile("../../migrations/rollback/mysql_0050_health_owner_range_start.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(rollback), ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := p.ExecContext(ctx, statement); err != nil {
			t.Fatal("rollback", err)
		}
	}
	if err := db.MigrateMySQL(ctx, p); err != nil {
		t.Fatal("reapply", err)
	}
	check(want, 0, want, 4*ownerRevisionRange)
	c.RecordID = "seven"
	if got := write(ctx, 1, []Sample{c}, true); got != 4*ownerRevisionRange+1 {
		t.Fatal(got)
	}
}
