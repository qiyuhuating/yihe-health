package rekey

import (
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/health"
)

func TestRekeyHealthHistoryReference(t *testing.T) {
	p := dbtest.Fixture(t)
	ctx := t.Context()
	old, current, retired, _ := boxes(t)
	at := time.Now().UTC().Truncate(time.Microsecond)
	sample := health.Sample{RecordID: "ref", Source: "watch", SourceDevice: "a", Metric: "heart_rate", Unit: "count/min", Value: health.Number(70), Start: at, End: at, Version: 1}
	batch, err := health.PrepareBatch([]health.Sample{sample})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err = batch.SaveProtected(ctx, tx, health.Owner{OldID: 1}, old); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	options := Options{Apply: true, BatchSize: 32, MaxRowsPerColumn: 1000}
	report, err := Run(ctx, p.DB, current, old, options)
	if err != nil || !report.EncryptedColumnsClean {
		t.Fatal("rotation", report.Stage, err)
	}
	var reference string
	if err = p.QueryRowContext(ctx, "SELECT payload_cipher FROM health_sample").Scan(&reference); err != nil || reference != "\x01" {
		t.Fatal("reference rewritten", err)
	}
	if got, err := health.ListChanges(ctx, p, 1, 0, 10, retired); err != nil || len(got.Items) != 1 {
		t.Fatal("rotated history", got, err)
	}
	got, _, _, _, err := health.LoadStatisticsSamples(ctx, p, 1, health.StatisticsQuery{Source: "watch", SourceDevice: "a", Metric: "heart_rate", Semantics: "instant", From: at.Add(-time.Second), To: at.Add(time.Second)}, retired, false)
	if err != nil || len(got) != 1 {
		t.Fatal("rotated current sample", got, err)
	}
	options.Apply = false
	if _, err = p.ExecContext(ctx, "DELETE FROM health_change"); err != nil {
		t.Fatal(err)
	}
	if _, err = Run(ctx, p.DB, current, old, options); err == nil {
		t.Fatal("missing reference accepted by rekey")
	}
}
