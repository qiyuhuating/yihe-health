package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

func TestDailyInvalidationDoesNotWaitOnOtherOwner(t *testing.T) {
	p := dbtest.Fixture(t)
	ctx := context.Background()
	for i := 1; i <= 128; i++ {
		_, err := p.ExecContext(ctx, "INSERT INTO metric_daily(cache_key,old_id,source,source_device,metric,semantics,record_day,offset_minutes,dirty) VALUES(?,?,'watch','A','heart_rate','instant','2026-10-22',0,?)", fmt.Sprintf("%064x", i), i, i > 2)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.ExecContext(ctx, "ANALYZE TABLE metric_daily"); err != nil {
		t.Fatal(err)
	}
	blocker, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var key string
	if err = blocker.QueryRowContext(ctx, "SELECT cache_key FROM metric_daily FORCE INDEX(idx_daily_dirty) WHERE dirty=false AND cache_key=? FOR UPDATE", fmt.Sprintf("%064x", 2)).Scan(&key); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := p.BeginTx(bounded, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	sample := Sample{RecordID: "independent", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: Number(72), Start: at, End: at, Version: 1}
	if _, _, err = SaveBatch(bounded, tx, Owner{OldID: 1}, []Sample{sample}); err != nil {
		t.Fatal("owner 1 blocked by unrelated dirty index row", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var dirty bool
	if err = p.QueryRowContext(ctx, "SELECT dirty FROM metric_daily WHERE old_id=1").Scan(&dirty); err != nil || !dirty {
		t.Fatal("owner 1 not invalidated", dirty, err)
	}
	if err = p.QueryRowContext(ctx, "SELECT dirty FROM metric_daily WHERE old_id=2").Scan(&dirty); err != nil || dirty {
		t.Fatal("unrelated owner modified", dirty, err)
	}
}

func TestDailySnapshotRejectsConcurrentCorrection(t *testing.T) {
	t.Setenv("MYSQL_STMT_CACHE_SIZE", "0")
	p := dbtest.Fixture(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	k := DailyKey{OldID: 1, Source: "watch", SourceDevice: "A", Metric: "heart_rate", Semantics: "instant", Day: "2026-10-22"}
	id := k.ID()
	if _, err := p.ExecContext(ctx, "INSERT INTO metric_daily(cache_key,old_id,source,source_device,metric,semantics,record_day,offset_minutes) VALUES(?,?,?,?,?,?,?,?)", id, k.OldID, k.Source, k.SourceDevice, k.Metric, k.Semantics, k.Day, k.OffsetMinutes); err != nil {
		t.Fatal(err)
	}
	sample := Sample{RecordID: "correct", Source: k.Source, SourceDevice: k.SourceDevice, Metric: k.Metric, Semantics: k.Semantics, Unit: "count/min", Value: Number(72), Start: at, End: at, Version: 1}
	save := func(v int64, n float64) {
		t.Helper()
		sample.Version, sample.Value = v, Number(n)
		tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, _, err = SaveBatch(ctx, tx, Owner{OldID: 1}, []Sample{sample}); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	save(1, 72)
	blocker, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	if _, err = LockOwner(ctx, blocker, 1); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	result, fallback, err := computeDailySnapshot(bounded, p, id, nil)
	cancel()
	if err != nil || fallback || result == nil {
		t.Fatal("snapshot waited for owner lock", fallback, err)
	}
	if err = blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	save(2, 95)
	done, err := publishDailySnapshot(ctx, p, id, nil, result)
	if err != nil || done {
		t.Fatal("stale result accepted", done, err)
	}
	var dirty bool
	if err = p.QueryRowContext(ctx, "SELECT dirty FROM metric_daily WHERE cache_key=?", id).Scan(&dirty); err != nil || !dirty {
		t.Fatal("dirty state lost", dirty, err)
	}
	if err = RefreshDaily(ctx, p, id, nil); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	var revision int64
	if err = p.QueryRowContext(ctx, "SELECT dirty,computed_revision,result FROM metric_daily WHERE cache_key=?", id).Scan(&dirty, &revision, &raw); err != nil {
		t.Fatal(err)
	}
	var out Statistics
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	current, err := StatisticsRevision(ctx, p, 1)
	if err != nil || dirty || revision != current || out.Mean == nil || *out.Mean != 95 {
		t.Fatalf("bad publication revision=%d current=%d dirty=%v stats=%+v err=%v", revision, current, dirty, out, err)
	}
}
