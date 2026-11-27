package health

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestLatestBackfillMatchesIncrementalTieBreak(t *testing.T) {
	database := dbtest.Fixture(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'tie-break')"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := Sample{RecordID: "first", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: Number(65), Start: now, End: now, Version: 1}
	second := first
	second.RecordID = "second"
	second.Value = Number(75)
	save := func(samples ...Sample) {
		t.Helper()
		tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, _, err = SaveBatch(ctx, tx, Owner{OldID: 1}, samples); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	check := func() {
		t.Helper()
		items, err := Latest(ctx, database, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Sample.RecordID != "second" {
			t.Fatalf("latest tie-break=%+v, want second", items)
		}
	}
	save(first, second)
	check() // Initial backfill selects highest sample_id on an exact timestamp/revision tie.
	stale := first
	stale.RecordID = "stale"
	stale.Start = now.Add(-time.Hour)
	stale.End = stale.Start
	save(stale)
	check() // Incremental refresh must select the same winner.
}

func TestLatestBackfillCapsDistinctSeriesRatherThanTiedSamples(t *testing.T) {
	database := dbtest.Fixture(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'latest-tie-cap')"); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Truncate(time.Microsecond)
	sample := Sample{
		RecordID: "tie-seed", Source: "watch", SourceDevice: "A",
		Metric: "heart_rate", Unit: "count/min", Value: Number(65),
		Start: start, End: start, Version: 1,
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{sample}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Simulate 10,000 imported records with one exact series/timestamp.
	// Assign distinct revisions to satisfy the per-owner uniqueness constraint.
	// RANK() returned every tied row and hit the candidate cap even
	// though the resident has only one distinct latest series.
	digits := "(SELECT 0 AS n UNION ALL SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3 UNION ALL SELECT 4 UNION ALL SELECT 5 UNION ALL SELECT 6 UNION ALL SELECT 7 UNION ALL SELECT 8 UNION ALL SELECT 9)"
	query := fmt.Sprintf(`INSERT INTO health_sample
		(sample_id,old_id,device_id,epoch,version,revision,payload,payload_cipher,source,source_device,metric,semantics,sample_start,sample_end,deleted)
		SELECT UNHEX(SHA2(CONCAT('latest-tie:',a.n,b.n,c.n,d.n),256)),
		h.old_id,h.device_id,h.epoch,h.version,
		h.revision + 1 + a.n * 1000 + b.n * 100 + c.n * 10 + d.n,
		h.payload,h.payload_cipher,
		h.source,h.source_device,h.metric,h.semantics,h.sample_start,h.sample_end,h.deleted
		FROM health_sample AS h
		CROSS JOIN %s AS a CROSS JOIN %s AS b CROSS JOIN %s AS c CROSS JOIN %s AS d
		WHERE h.old_id=1`, digits, digits, digits, digits)
	if _, err := database.ExecContext(ctx, query); err != nil {
		t.Fatal(err)
	}
	var candidates int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample WHERE old_id=1").Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 10001 {
		t.Fatalf("expected 10,001 tied candidates, got %d", candidates)
	}
	records, err := Latest(ctx, database, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Sample.RecordID != sample.RecordID {
		t.Fatalf("latest tied series=%+v, want the single series", records)
	}
}
