package health

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestLatestSnapshotTracksOutOfOrderAndTombstones(t *testing.T) {
	database := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := database.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'latest')"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	older := Sample{RecordID: "older", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: Number(65), Start: now, End: now, Version: 1}
	newer := older
	newer.RecordID = "newer"
	newer.Start = now.Add(time.Minute)
	newer.End = newer.Start
	newer.Value = Number(75)
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
	assertLatest := func(want string) {
		t.Helper()
		got, err := Latest(ctx, database, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if len(got) != 0 {
				t.Fatalf("expected empty latest, got %+v", got)
			}
		} else if len(got) != 1 || got[0].Sample.RecordID != want {
			t.Fatalf("latest=%+v, want %s", got, want)
		}
	}
	save(older, newer)
	assertLatest("newer") // first read completes owner-locked backfill
	stale := older
	stale.RecordID = "stale"
	stale.Start = now.Add(-time.Hour)
	stale.End = stale.Start
	save(stale)
	assertLatest("newer") // older packet cannot displace snapshot
	newer.Deleted = true
	newer.Version = 2
	newer.Value = nil
	save(newer)
	assertLatest("older") // deletion falls back to previous sample
	older.Deleted = true
	older.Version = 2
	older.Value = nil
	save(older)
	assertLatest("stale")
	stale.Deleted = true
	stale.Version = 2
	stale.Value = nil
	save(stale)
	assertLatest("")
}

func TestLatestKeySeparatesSeriesFields(t *testing.T) {
	a := Sample{Source: "ab", SourceDevice: "c", Metric: "heart_rate", Semantics: "instant"}
	b := Sample{Source: "a", SourceDevice: "bc", Metric: "heart_rate", Semantics: "instant"}
	c := Sample{Source: "Ab", SourceDevice: "c", Metric: "heart_rate", Semantics: "instant"}
	if latestKey(a) == latestKey(b) || latestKey(a) == latestKey(c) {
		t.Fatal("distinct series share the same key")
	}
}
