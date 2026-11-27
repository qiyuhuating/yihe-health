package health

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestDailyReuseProtectedRows(t *testing.T) {
	p := dbtest.Fixture(t)
	ctx := t.Context()
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'daily reuse')"); err != nil {
		t.Fatal(err)
	}
	k := DailyKey{OldID: 1, Source: "watch", SourceDevice: "A", Metric: "heart_rate", Semantics: "instant", Day: "2026-10-22"}
	id := k.ID()
	if _, err = p.ExecContext(ctx, "INSERT INTO metric_daily(cache_key,old_id,source,source_device,metric,semantics,record_day,offset_minutes) VALUES(?,?,?,?,?,?,?,?)", id, k.OldID, k.Source, k.SourceDevice, k.Metric, k.Semantics, k.Day, k.OffsetMinutes); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	save := func(key string, value float64, b *securestore.Box) {
		t.Helper()
		sample := Sample{RecordID: key, Source: k.Source, SourceDevice: k.SourceDevice, Metric: k.Metric, Semantics: k.Semantics, Unit: "count/min", Value: Number(value), Start: at, End: at, Version: 1}
		batch, err := PrepareBatch([]Sample{sample})
		if err != nil {
			t.Fatal(err)
		}
		tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, _, err = batch.SaveProtected(ctx, tx, Owner{OldID: 1}, b); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	save("first", 60, box)
	save("plain", 75, nil)
	save("long-"+string(bytes.Repeat([]byte{'a'}, 100)), 90, box)
	if err = RefreshDaily(ctx, p, id, box); err != nil {
		t.Fatal(err)
	}
	var revision int64
	var stored ProjectionPayload
	if err = p.QueryRowContext(ctx, "SELECT computed_revision,result,result_cipher FROM metric_daily WHERE cache_key=?", id).Scan(&revision, &stored.JSON, &stored.Cipher); err != nil {
		t.Fatal(err)
	}
	raw, err := stored.OpenDailyResult(box, id, revision)
	if err != nil {
		t.Fatal(err)
	}
	var result Statistics
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Count != 3 || result.Mean == nil || *result.Mean != 75 || result.Min == nil || *result.Min != 60 || result.Max == nil || *result.Max != 90 {
		t.Fatalf("incorrect aggregation: %+v", result)
	}
	if _, err = p.ExecContext(ctx, "UPDATE health_sample SET payload_cipher='invalid' WHERE payload_cipher IS NOT NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE metric_daily SET dirty=true WHERE cache_key=?", id); err != nil {
		t.Fatal(err)
	}
	if err = RefreshDaily(ctx, p, id, box); err == nil {
		t.Fatal("tampered payload accepted")
	}
	var dirty bool
	if err = p.QueryRowContext(ctx, "SELECT dirty FROM metric_daily WHERE cache_key=?", id).Scan(&dirty); err != nil || !dirty {
		t.Fatalf("invalid result published: dirty=%v err=%v", dirty, err)
	}
}
