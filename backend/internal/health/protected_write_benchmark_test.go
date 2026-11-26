package health

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func protectedWriteFixture(t testing.TB) (*db.Database, *securestore.Box, []Sample) {
	t.Helper()
	p := dbtest.Fixture(t)
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExecContext(context.Background(), "INSERT INTO oldperson(old_id,name) VALUES(1,'protected batch')"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	samples := make([]Sample, 128)
	for i := range samples {
		ts := at.Add(time.Duration(i) * time.Second)
		samples[i] = Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: Number(float64(60 + i%31)), Start: ts, End: ts, Version: 1}
	}
	return p, box, samples
}

func saveProtectedFixture(ctx context.Context, p *db.Database, box *securestore.Box, samples []Sample) (int, error) {
	batch, err := PrepareBatch(samples)
	if err != nil {
		return 0, err
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, duplicates, err := batch.SaveProtected(ctx, tx, Owner{OldID: 1}, box)
	if err != nil {
		return 0, err
	}
	return duplicates, tx.Commit()
}

func TestProtectedWriteLookupAuthenticatesObsoleteRows(t *testing.T) {
	p, box, samples := protectedWriteFixture(t)
	ctx := t.Context()
	a, b := samples[0], samples[1]
	b.Version = 3
	if d, err := saveProtectedFixture(ctx, p, box, []Sample{a, b}); err != nil || d != 0 {
		t.Fatal(d, err)
	}
	a.Version = 2
	if d, err := saveProtectedFixture(ctx, p, box, []Sample{a, b}); err != nil || d != 1 {
		t.Fatal(d, err)
	}
	b.Value = Number(99)
	if _, err := saveProtectedFixture(ctx, p, box, []Sample{a, b}); !errors.Is(err, ErrConflict) {
		t.Fatal("equal-version conflict not detected", err)
	}
	b.Value = samples[1].Value
	if _, err := p.ExecContext(ctx, "UPDATE health_sample SET payload_cipher='invalid' WHERE sample_id=UNHEX(?)", identityDigest(Owner{OldID: 1}, a)); err != nil {
		t.Fatal(err)
	}
	a.Version = 4
	b.Version = 4
	if _, err := saveProtectedFixture(ctx, p, box, []Sample{a, b}); err == nil {
		t.Fatal("corrupt obsolete ciphertext accepted")
	}
	var count int
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_change").Scan(&count); err != nil || count != 3 {
		t.Fatal("failed batch changed history", count, err)
	}
}

// Includes actual SQL, authenticated payloads, initialized latest projections,
// and durable COMMIT; the isolated fixture and first batch are not timed.
func BenchmarkProtectedWriteBatch(b *testing.B) {
	for _, mode := range []string{"fresh", "revision"} {
		b.Run(mode, func(b *testing.B) {
			p, box, samples := protectedWriteFixture(b)
			ctx := b.Context()
			if d, err := saveProtectedFixture(ctx, p, box, samples); err != nil || d != 0 {
				b.Fatal(d, err)
			}
			if got, err := Latest(ctx, p, 1, box); err != nil || len(got) != 1 {
				b.Fatal("latest", len(got), err)
			}
			b.ReportAllocs()
			generation := 1
			at := samples[0].Start
			for b.Loop() {
				generation++
				for i := range samples {
					if mode == "fresh" {
						samples[i].RecordID = fmt.Sprintf("%d:%d", generation, i)
						ts := at.Add(time.Duration(generation*len(samples)+i) * time.Second)
						samples[i].Start, samples[i].End = ts, ts
					} else {
						samples[i].Version = int64(generation)
					}
				}
				if d, err := saveProtectedFixture(ctx, p, box, samples); err != nil || d != 0 {
					b.Fatal(d, err)
				}
			}
			var count int
			if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_change").Scan(&count); err != nil || count != generation*len(samples) {
				b.Fatalf("durable history=%d want=%d err=%v", count, generation*len(samples), err)
			}
			want := len(samples)
			if mode == "fresh" {
				want *= generation
			}
			if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample").Scan(&count); err != nil || count != want {
				b.Fatalf("durable samples=%d want=%d err=%v", count, want, err)
			}
		})
	}
}
