package health

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

// BenchmarkDailyProtectedSQL includes the actual MySQL row scan, authenticated
// payload decode, aggregation and result seal. Fixture setup is not timed.
func BenchmarkDailyProtectedSQL(b *testing.B) {
	p := dbtest.Fixture(b)
	ctx := b.Context()
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	if err != nil {
		b.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'daily benchmark')"); err != nil {
		b.Fatal(err)
	}
	k := DailyKey{OldID: 1, Source: "watch", SourceDevice: "A", Metric: "heart_rate", Semantics: "instant", Day: "2026-10-22"}
	id := k.ID()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for group := range 8 {
		samples := make([]Sample, 128)
		for i := range samples {
			n := group*len(samples) + i
			ts := at.Add(time.Duration(n) * time.Second)
			samples[i] = Sample{RecordID: fmt.Sprintf("daily-%04d", n), Source: k.Source, SourceDevice: k.SourceDevice, Metric: k.Metric, Semantics: k.Semantics, Unit: "count/min", Value: Number(float64(60 + n%31)), Start: ts, End: ts, Version: 1}
		}
		batch, err := PrepareBatch(samples)
		if err != nil {
			b.Fatal(err)
		}
		tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			b.Fatal(err)
		}
		_, _, err = batch.SaveProtected(ctx, tx, Owner{OldID: 1}, box)
		if err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	revision, err := StatisticsRevision(ctx, p, 1)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			b.Fatal(err)
		}
		result, message, err := computeDaily(ctx, tx, id, k, revision, box)
		rollbackErr := tx.Rollback()
		if err != nil || rollbackErr != nil || message != "" || result.Cipher == nil {
			b.Fatalf("aggregate error: %v %v %s", err, rollbackErr, message)
		}
	}
}
