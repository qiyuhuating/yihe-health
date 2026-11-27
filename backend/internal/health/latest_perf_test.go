package health

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

// A ready latest projection with a long history; every measured transaction
// corrects an older observation without changing the current winner.
func BenchmarkLatestHistoricalCorrection(b *testing.B) {
	d := dbtest.Fixture(b)
	ctx := context.Background()
	if _, err := d.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')"); err != nil {
		b.Fatal(err)
	}
	box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	makeSample := func(i int) Sample {
		tm := at.Add(time.Duration(i) * time.Second)
		return Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "synthetic", Metric: "heart_rate", Semantics: "instant", Unit: "count/min", Value: Number(72), Start: tm, End: tm, Version: 1}
	}
	save := func(samples []Sample) {
		batch, err := PrepareBatch(samples)
		if err != nil {
			b.Fatal(err)
		}
		tx, err := d.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			b.Fatal(err)
		}
		defer tx.Rollback()
		if _, _, err = batch.SaveProtected(ctx, tx, Owner{OldID: 1}, box); err != nil {
			b.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			b.Fatal(err)
		}
	}
	for start := 0; start < 4096; start += 128 {
		samples := make([]Sample, 128)
		for i := range samples {
			samples[i] = makeSample(start + i)
		}
		save(samples)
	}
	if _, err := Latest(ctx, d, 1, box); err != nil {
		b.Fatal(err)
	}
	status := func() map[string]uint64 {
		rows, err := d.QueryContext(ctx, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Bytes_sent','Innodb_rows_read','Innodb_os_log_written','Com_delete','Com_insert','Com_update')")
		if err != nil {
			b.Fatal(err)
		}
		defer rows.Close()
		m := map[string]uint64{}
		for rows.Next() {
			var k string
			var v uint64
			if err := rows.Scan(&k, &v); err != nil {
				b.Fatal(err)
			}
			m[k] = v
		}
		if err := rows.Err(); err != nil {
			b.Fatal(err)
		}
		return m
	}
	before := status()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s := makeSample(3)
		s.Version = int64(i) + 2
		s.Value = Number(float64(60 + i%30))
		save([]Sample{s})
	}
	b.StopTimer()
	after := status()
	for k, v := range after {
		if v >= before[k] {
			b.ReportMetric(float64(v-before[k])/float64(b.N), k+"/op")
		}
	}
	latest, err := Latest(ctx, d, 1, box)
	if err != nil || len(latest) != 1 || latest[0].Sample.RecordID != "4095" {
		b.Fatalf("latest changed: %v %v", latest, err)
	}
}
