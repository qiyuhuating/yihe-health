package health

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"yihe-care/internal/db/dbtest"
)

func TestIndexedBatchPersistsAllVersionsAndTombstones(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'layout')"); err != nil {
		t.Fatal(err)
	}
	_, base := sampleBenchmarkInput()
	samples := make([]Sample, 200)
	for i := range samples {
		s := base
		s.RecordID = fmt.Sprint(i % 50)
		s.Version = int64(i/50 + 1)
		s.Value = Number(float64(60 + s.Version))
		if s.Version == 4 && i%7 == 0 {
			s.Deleted = true
			s.Value = nil
		}
		samples[i] = s
	}
	save := func(wantDuplicates int) {
		tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		rev, duplicates, err := SaveBatch(ctx, tx, Owner{OldID: 1}, samples)
		if err != nil || rev != 200 || duplicates != wantDuplicates {
			t.Fatalf("rev=%d duplicates=%d err=%v", rev, duplicates, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	save(0)
	save(200)
	rows, err := pool.QueryContext(ctx, "SELECT revision,payload FROM health_change ORDER BY revision")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var revision int64
		var payload []byte
		var record Record
		if err := rows.Scan(&revision, &payload); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(payload, &record); err != nil {
			t.Fatal(err)
		}
		want := samples[count]
		if revision != int64(count+1) || record.Revision != revision || record.Sample.RecordID != want.RecordID || record.Sample.Version != want.Version || record.Sample.Deleted != want.Deleted {
			t.Fatalf("history %d differs: %+v", count, record)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if count != 200 {
		t.Fatalf("history collapsed to %d", count)
	}
	rows, err = pool.QueryContext(ctx, "SELECT revision,payload FROM health_sample ORDER BY revision")
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for rows.Next() {
		var revision int64
		var payload []byte
		var sample Sample
		if err := rows.Scan(&revision, &payload); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(payload, &sample); err != nil {
			t.Fatal(err)
		}
		if sample.Version != 4 || revision != int64(151+count) || sample.Deleted != samples[150+count].Deleted {
			t.Fatalf("final version differs: %d %+v", revision, sample)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if count != 50 {
		t.Fatalf("final count %d", count)
	}
}

func TestConditionalPreviousPayloadPreservesMixedVersionsAndConflicts(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'pruned')"); err != nil {
		t.Fatal(err)
	}
	_, a := sampleBenchmarkInput()
	a.RecordID = "a"
	a.Version = 4
	a.Metadata = map[string]string{"large": strings.Repeat("x", 1024)}
	b := a
	b.RecordID = "b"
	b.Version = 12
	owner := Owner{OldID: 1}
	save := func(samples []Sample) (int64, int, error) {
		tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		rev, duplicates, err := SaveBatch(ctx, tx, owner, samples)
		if err == nil {
			err = tx.Commit()
		}
		return rev, duplicates, err
	}
	if _, d, err := save([]Sample{a, b}); err != nil || d != 0 {
		t.Fatal(d, err)
	}
	rows, err := pool.QueryContext(ctx, batchSQL(batchLookup, 2), int64(10), identityDigest(owner, a), identityDigest(owner, b))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for rows.Next() {
		var id string
		var prior storedSample
		var ciphertext *string
		if err := rows.Scan(&id, &prior.version, &prior.revision, &prior.payload, &ciphertext); err != nil {
			t.Fatal(err)
		}
		if ciphertext != nil {
			t.Fatal("legacy writer unexpectedly added ciphertext")
		}
		if prior.version == 4 && prior.payload != nil {
			t.Fatal("obsolete JSON was transferred")
		}
		if prior.version == 12 && !bytes.Contains(prior.payload, []byte("large")) {
			t.Fatal("needed conflict payload was omitted")
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if seen != 2 {
		t.Fatal(seen)
	}
	a.Version = 5
	if rev, d, err := save([]Sample{a, b}); err != nil || d != 1 || rev != 3 {
		t.Fatal("mixed update/replay changed", rev, d, err)
	}
	a.Version = 6
	b.Value = Number(199)
	if _, _, err := save([]Sample{a, b}); !errors.Is(err, ErrConflict) {
		t.Fatal("equal-version conflict lost", err)
	}
	var count, version int64
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM health_change").Scan(&count); err != nil || count != 3 {
		t.Fatal("failed batch persisted history", count, err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT version FROM health_sample WHERE sample_id=?", identityDigest(owner, a)).Scan(&version); err != nil || version != 5 {
		t.Fatal("failed batch persisted current row", version, err)
	}
}
