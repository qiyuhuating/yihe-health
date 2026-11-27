package health

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestLatestSnapshotTracksOutOfOrderAndTombstones(t *testing.T) {
	database := dbtest.Fixture(t)
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

func TestLatestRefreshMultipleSeriesInOneBatch(t *testing.T) {
	database := dbtest.Fixture(t)
	ctx := t.Context()
	if _, err := database.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'multi-series')"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sample := Sample{RecordID: "first", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: Number(65), Start: now, End: now, Version: 1}
	other := sample
	other.SourceDevice = "B"
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
	save(sample, other)
	if got, err := Latest(ctx, database, 1, nil); err != nil || len(got) != 2 {
		t.Fatalf("initial projection: %v %v", got, err)
	}
	newer := sample
	newer.RecordID, newer.Start, newer.End = "newer", now.Add(time.Minute), now.Add(time.Minute)
	other.Deleted, other.Value, other.Version = true, nil, 2
	third := sample
	third.SourceDevice = "C"
	save(newer, other, third)
	got, err := Latest(ctx, database, 1, nil)
	if err != nil || len(got) != 2 || got[0].Sample.SourceDevice != "A" || got[0].Sample.RecordID != "newer" || got[1].Sample.SourceDevice != "C" {
		t.Fatalf("updated/deleted/inserted projection: %v %v", got, err)
	}
}

func TestStatisticsCommittedSnapshot(t *testing.T) {
	database := dbtest.Fixture(t)
	ctx := t.Context()
	if _, err := database.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'snapshot')"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sample := Sample{RecordID: "snapshot", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Value: Number(65), Start: now, End: now, Version: 1}
	save := func(s Sample) {
		t.Helper()
		tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, _, err := SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{s}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	save(sample)
	snapshot, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Rollback()
	revision, err := StatisticsRevision(ctx, snapshot, 1)
	if err != nil || revision <= 0 {
		t.Fatalf("revision=%d: %v", revision, err)
	}
	sample.Version, sample.Value = 2, Number(83)
	save(sample)
	query := StatisticsQuery{Source: "watch", SourceDevice: "A", Metric: "heart_rate", Semantics: "instant", From: now.Add(-time.Second), To: now.Add(time.Second)}
	samples, _, _, _, err := LoadStatisticsSamples(ctx, snapshot, 1, query, nil, true)
	if err != nil || len(samples) != 1 || *samples[0].Value != 65 {
		t.Fatalf("snapshot changed after concurrent commit: %v %v", samples, err)
	}
	if r, err := StatisticsRevision(ctx, snapshot, 1); err != nil || r != revision {
		t.Fatalf("snapshot clock changed: %d %v", r, err)
	}
	if err := snapshot.Commit(); err != nil {
		t.Fatal(err)
	}
	if r, err := StatisticsRevision(ctx, database, 1); err != nil || r <= revision {
		t.Fatalf("new read missed committed revision: %d %v", r, err)
	}
	samples, _, _, _, err = LoadStatisticsSamples(ctx, database, 1, query, nil, true)
	if err != nil || len(samples) != 1 || *samples[0].Value != 83 {
		t.Fatalf("new read missed committed correction: %v %v", samples, err)
	}
}

func TestLatestColdAndWarmAgreeOnSubMicrosecondTies(t *testing.T) {
	database := dbtest.Fixture(t)
	ctx := t.Context()
	if _, err := database.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'exact time')"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	later := Sample{RecordID: "later", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: Number(75), Start: at.Add(900 * time.Nanosecond), Version: 1}
	later.End = later.Start
	earlier := later
	earlier.RecordID, earlier.Value = "earlier", Number(65)
	earlier.Start = at.Add(100 * time.Nanosecond)
	earlier.End = earlier.Start
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
		if err != nil || len(got) != 1 || got[0].Sample.RecordID != want {
			t.Fatalf("latest=%+v err=%v want=%s", got, err, want)
		}
	}
	// The earlier timestamp intentionally has the higher revision.
	save(later, earlier)
	assertLatest("later")
	earlier.Version++
	earlier.Value = Number(66)
	save(earlier)
	assertLatest("later")
	if _, err := database.ExecContext(ctx, "DELETE FROM health_sample_latest WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM health_latest_ready WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	assertLatest("later")
	// Exact timestamp equality still uses revision order.
	tied := later
	tied.RecordID = "same-nanos-new-revision"
	save(tied)
	assertLatest(tied.RecordID)
	if _, err := database.ExecContext(ctx, "DELETE FROM health_sample_latest WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM health_latest_ready WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	assertLatest(tied.RecordID)
}
