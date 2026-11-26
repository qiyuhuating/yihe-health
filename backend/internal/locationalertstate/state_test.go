package locationalertstate_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/locationalertstate"
	"yihe-care/internal/operationsrepo"
	"yihe-care/internal/telemetryrepo"
)

func TestLatestLocationAlertStateAndResolution(t *testing.T) {
	store := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := store.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'测试')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, "INSERT INTO device(device_id,device_no,secret,old_id,status,health_epoch) VALUES(1,'LOC001','test',1,1,1)"); err != nil {
		t.Fatal(err)
	}
	insert := func(at time.Time) int64 {
		tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		id, err := telemetryrepo.InsertAlarm(ctx, tx, telemetryrepo.Alarm{
			ResidentID: 1, DeviceID: 1, ObservedAt: at, Kind: "fall",
		})
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	first := insert(time.Now().Add(-time.Minute))
	second := insert(time.Now())
	total, active, err := operationsrepo.ReadPositionAlerts(ctx, store)
	if err != nil || total != 1 || active != 1 {
		t.Fatalf("before resolution total=%d active=%d err=%v", total, active, err)
	}
	resolved, err := telemetryrepo.ResolveLatestLocationAlert(ctx, store, 1)
	if err != nil || !resolved {
		t.Fatalf("resolve=%v err=%v", resolved, err)
	}
	var firstStatus, secondStatus string
	if err = store.QueryRowContext(ctx, "SELECT status FROM alarm_precess WHERE id=?", first).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err = store.QueryRowContext(ctx, "SELECT status FROM alarm_precess WHERE id=?", second).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "1" || secondStatus != "0" {
		t.Fatalf("resolved wrong alarm: first=%s second=%s", firstStatus, secondStatus)
	}
	_, active, err = operationsrepo.ReadPositionAlerts(ctx, store)
	if err != nil || active != 0 {
		t.Fatalf("after resolution active=%d err=%v", active, err)
	}
	insert(time.Now().Add(time.Second))
	_, active, err = operationsrepo.ReadPositionAlerts(ctx, store)
	if err != nil || active != 1 {
		t.Fatalf("new alarm did not reactivate state: active=%d err=%v", active, err)
	}
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = locationalertstate.Resolve(ctx, tx, 1, first); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, active, err = operationsrepo.ReadPositionAlerts(ctx, store)
	if err != nil || active != 1 {
		t.Fatalf("older alarm resolution cleared the new alert: active=%d err=%v", active, err)
	}
}
