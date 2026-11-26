package health

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestOwnerClockConcurrentFirstUse(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var initial int64
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM health_owner_clock WHERE old_id=1").Scan(&initial); err != nil || initial != 0 {
		t.Fatal("owner must not exist before concurrent first use", initial, err)
	}
	const writers = 8
	type result struct {
		revision int64
		err      error
	}
	ready := make(chan struct{}, writers)
	start := make(chan struct{})
	results := make(chan result, writers)
	at := time.Now().UTC().Truncate(time.Microsecond)
	for i := range writers {
		go func(i int) {
			tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			ready <- struct{}{}
			<-start
			if err != nil {
				results <- result{err: err}
				return
			}
			defer tx.Rollback()
			s := Sample{RecordID: strconv.Itoa(i), Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: Number(70), Start: at, End: at, Version: 1}
			revision, _, err := Save(ctx, tx, Owner{OldID: 1}, s)
			if err == nil {
				err = tx.Commit()
			}
			results <- result{revision, err}
		}(i)
	}
	for range writers {
		<-ready
	}
	close(start)
	seen := make(map[int64]bool, writers)
	for range writers {
		r := <-results
		if r.err != nil {
			t.Error(r.err)
			continue
		}
		if r.revision < 1 || r.revision > writers || seen[r.revision] {
			t.Error("duplicate or skipped first-use revision", r.revision)
		}
		seen[r.revision] = true
	}
	var count, revision, rangeEnd, high int64
	if err := p.QueryRowContext(ctx, "SELECT count(*),max(revision),max(range_end) FROM health_owner_clock WHERE old_id=1").Scan(&count, &revision, &rangeEnd); err != nil || count != 1 || revision != writers || rangeEnd != ownerRevisionRange {
		t.Fatal("concurrent initialization lost owner state", count, revision, rangeEnd, err)
	}
	if err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != ownerRevisionRange {
		t.Fatal("first use allocated multiple ranges", high, err)
	}
	for _, table := range []string{"health_sample", "health_change"} {
		if err := p.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE old_id=1").Scan(&count); err != nil || count != writers {
			t.Fatal(table, count, err)
		}
	}
}

func TestOwnerClockAllowsIndependentCommits(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	s := Sample{RecordID: "one", Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: Number(70), Start: at, End: at, Version: 1}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if revision, _, err := Save(ctx, tx, Owner{OldID: 1}, s); err != nil || revision != 1 {
		t.Fatal(revision, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	s.Version = 2
	if revision, _, err := Save(ctx, tx, Owner{OldID: 1}, s); err != nil || revision != 2 {
		t.Fatal(revision, err)
	}
	// Owner 1's uncommitted event must neither block owner 2 nor appear in a
	// reader's owner-1 cursor page before that transaction commits.
	otherCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	other, err := p.BeginTx(otherCtx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback()
	if revision, _, err := Save(otherCtx, other, Owner{OldID: 2}, s); err != nil || revision != ownerRevisionRange+1 {
		t.Fatal("independent owner blocked", revision, err)
	}
	if err = other.Commit(); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM health_change WHERE old_id=1 AND revision>1").Scan(&count); err != nil || count != 0 {
		t.Fatal("uncommitted event visible", count, err)
	}
	blocked, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	same, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer same.Rollback()
	if _, err = LockOwner(blocked, same, 1); err == nil {
		t.Fatal("same owner bypassed commit ordering")
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM health_change WHERE old_id=1 AND revision>1").Scan(&count); err != nil || count != 1 {
		t.Fatal("committed event missing from owner cursor", count, err)
	}
}

func TestOwnerRangeRollbackAndSafeCursorLimit(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	s := Sample{RecordID: "one", Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: Number(70), Start: at, End: at, Version: 1}
	// The final range is truncated at JavaScript's largest exact integer.
	if _, err := p.ExecContext(ctx, "UPDATE health_clock SET revision=? WHERE id=1", maxSafeRevision-2); err != nil {
		t.Fatal(err)
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if revision, _, err := Save(ctx, tx, Owner{OldID: 1}, s); err != nil || revision != maxSafeRevision-1 {
		t.Fatal(revision, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var high int64
	if err = p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != maxSafeRevision-2 {
		t.Fatal("rolled-back reservation advanced high-water mark", high, err)
	}
	tx, err = p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	second := s
	second.RecordID = "two"
	if revision, _, err := SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{s, second}); err != nil || revision != maxSafeRevision {
		t.Fatal(revision, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	s.Version = 2
	if _, _, err = Save(ctx, tx, Owner{OldID: 1}, s); err == nil {
		t.Fatal("unsafe cursor revision accepted")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != maxSafeRevision {
		t.Fatal(high, err)
	}
}

func TestOwnerRangeRefillPreservesVersionHistory(t *testing.T) {
	p := dbtest.MySQL(t)
	ctx := context.Background()
	at := time.Now().UTC().Truncate(time.Microsecond)
	s := Sample{RecordID: "one", Source: "sim", SourceDevice: "sim", Metric: "heart_rate", Unit: "count/min", Value: Number(70), Start: at, End: at, Version: 1}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err = Save(ctx, tx, Owner{OldID: 1}, s); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE health_owner_clock SET revision=range_end-1 WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	tx, err = p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	newer, newest := s, s
	newer.Version, newest.Version = 2, 3
	if revision, duplicates, err := SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{newer, newest, newer}); err != nil || revision != ownerRevisionRange+2 || duplicates != 1 {
		t.Fatal(revision, duplicates, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var count, high int64
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM health_change WHERE old_id=1").Scan(&count); err != nil || count != 3 {
		t.Fatal("refill lost an intermediate version", count, err)
	}
	if err = p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=1").Scan(&high); err != nil || high != ownerRevisionRange*2 {
		t.Fatal(high, err)
	}
	// A damaged high-water mark must never reissue a reserved range.
	if _, err = p.ExecContext(ctx, "UPDATE health_owner_clock SET revision=range_end WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE health_clock SET revision=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	tx, err = p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	s.Version = 4
	if _, _, err = Save(ctx, tx, Owner{OldID: 1}, s); err == nil {
		t.Fatal("overlapping range accepted after high-water mark regressed")
	}
}
