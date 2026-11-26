package operationsrepo

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/healthalertstate"
)

func TestHealthAlertSnapshotIsBindingScoped(t *testing.T) {
	store := dbtest.MySQL(t)
	ctx := context.Background()
	if _, err := store.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'测试'),(2,'新住户')"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecContext(ctx, "INSERT INTO device(device_id,device_no,secret,old_id,status,health_epoch,bind_time) VALUES(1,'SNAP001','test',1,1,1,?)", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err = healthalertstate.RecordVitals(ctx, tx, 1, 1, time.Now(), "1", "0"); err == nil {
		state := "1"
		err = healthalertstate.RecordSleep(ctx, tx, 1, 1, time.Now(), &state)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		t.Fatal(err)
	}
	counts, err := ReadHealthAlerts(ctx, store)
	if err != nil || counts != (HealthAlerts{HeartRate: 1, Sleep: 1}) {
		t.Fatalf("epoch 1 counts=%+v err=%v", counts, err)
	}
	if _, err = store.ExecContext(ctx, "UPDATE device SET old_id=2,bind_time=? WHERE device_id=1", time.Now()); err != nil {
		t.Fatal(err)
	}
	counts, err = ReadHealthAlerts(ctx, store)
	if err != nil || counts != (HealthAlerts{}) {
		t.Fatalf("stale binding leaked: counts=%+v err=%v", counts, err)
	}
	tx, err = store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err = healthalertstate.RecordVitals(ctx, tx, 1, 2, time.Now(), "0", "1"); err == nil {
		err = tx.Commit()
	}
	if err != nil {
		t.Fatal(err)
	}
	counts, err = ReadHealthAlerts(ctx, store)
	if err != nil || counts != (HealthAlerts{BloodPressure: 1}) {
		t.Fatalf("epoch 2 counts=%+v err=%v", counts, err)
	}
}
