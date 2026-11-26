package health

import (
	"database/sql"
	"testing"
	"time"
)

func TestLatestForwardBatchAvoidsRescan(t *testing.T) {
	p, box, samples := protectedWriteFixture(t)
	ctx := t.Context()
	if _, err := saveProtectedFixture(ctx, p, box, samples[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := Latest(ctx, p, 1, box); err != nil {
		t.Fatal(err)
	}
	conn, err := p.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	selects := func() int64 {
		t.Helper()
		var name string
		var n int64
		if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	write := func(s []Sample) int64 {
		t.Helper()
		b, err := PrepareBatch(s)
		if err != nil {
			t.Fatal(err)
		}
		before := selects()
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, d, err := b.SaveProtected(ctx, tx, Owner{OldID: 1}, box); err != nil || d != 0 {
			t.Fatal(d, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return selects() - before
	}
	// Two tied indexed timestamps, with input order opposing true nanoseconds.
	a, b := samples[2], samples[3]
	a.Start = samples[1].Start.Add(time.Second + 900*time.Nanosecond)
	a.End = a.Start
	b.Start = a.Start.Add(-500 * time.Nanosecond)
	b.End = b.Start
	n := write([]Sample{a, b})
	if n != 3 {
		t.Errorf("forward batch SELECTs=%d want 3", n)
	}
	assertWinner := func(want string) {
		t.Helper()
		got, err := Latest(ctx, p, 1, box)
		if err != nil || len(got) != 1 || got[0].Sample.RecordID != want {
			t.Fatal("winner", got, err)
		}
	}
	assertWinner(a.RecordID)
	a.Version++
	a.Deleted = true
	a.Value = nil
	if n = write([]Sample{a}); n != 4 {
		t.Errorf("tombstone SELECTs=%d want 4", n)
	}
	assertWinner(b.RecordID)

	// Same indexed microsecond is deliberately not eligible for the fast path.
	c := samples[4]
	c.Start = b.Start.Add(100 * time.Nanosecond)
	c.End = c.Start
	if n = write([]Sample{c}); n != 4 {
		t.Errorf("same-microsecond SELECTs=%d want 4", n)
	}
	assertWinner(c.RecordID)

	// An invalid prior envelope must still abort the whole forward batch.
	if _, err := p.ExecContext(ctx, "UPDATE health_sample_latest SET payload_cipher='invalid' WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_change").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := saveProtectedFixture(ctx, p, box, samples[5:6]); err == nil {
		t.Fatal("corrupt latest envelope accepted")
	}
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_change").Scan(&after); err != nil || before != after {
		t.Fatal("partial write", before, after, err)
	}
}
