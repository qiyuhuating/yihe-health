package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOrdinaryReportRejectsStaleBindingVersionWithoutPersisting(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	// MySQL binding triggers advance health_epoch on old_id changes;
	// direct assignment to health_epoch is deliberately ignored.
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	mustExec(t, pool, "UPDATE device SET old_id=1 WHERE device_id=1")
	stale := callAPI(h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"bindingVersion": 1, "hr": 74, "step": 12,
		"lng": 113.0625, "lat": 28.1975,
	})
	if stale.Code != http.StatusConflict {
		t.Fatalf("ordinary stale report returned HTTP %d: %s", stale.Code, stale.Body.String())
	}
	ctx := context.Background()
	for _, table := range []string{"ratepre", "watch_day_info", "device_location"} {
		var count int
		if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s persisted a stale sample: %d err=%v", table, count, err)
		}
	}
	current := callAPI(h.deviceReport, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"bindingVersion": 3, "hr": 74, "step": 12,
	})
	if current.Code != http.StatusOK {
		t.Fatalf("current report failed: HTTP %d %s", current.Code, current.Body.String())
	}
}

func TestOrdinaryReportConcurrentRebindCannotWriteOldOwner(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'new owner')")
	h := testHandler(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var ownerID int64
	if err := tx.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- callAPI(h.deviceReport, map[string]any{
			"deviceNo": "WATCH001", "secret": testSecret,
			"hr": 76, "step": 100, "lng": 113.0625, "lat": 28.1975,
		})
	}()
	// Do not rely on a sleep or scheduler timing: wait until the device
	// request is demonstrably blocked on the old owner's row.
	waits := `SELECT count(*) FROM performance_schema.data_lock_waits w
		JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE
		WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME='oldperson'`
	for {
		var count int
		if err := pool.QueryRowContext(ctx, waits).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("ordinary report did not wait for its original owner: %v", ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE device SET old_id=2 WHERE device_id=1"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-result:
		if response.Code != http.StatusConflict {
			t.Fatalf("report crossed rebind: HTTP %d %s", response.Code, response.Body.String())
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for _, table := range []string{"ratepre", "watch_day_info", "device_location"} {
		var count int
		if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s saved data from previous binding: count=%d err=%v", table, count, err)
		}
	}
}
