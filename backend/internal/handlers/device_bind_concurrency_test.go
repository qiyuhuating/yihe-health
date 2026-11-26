package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// An administrator may disable or rename a device while its bind request is
// blocked on the elder row. The eventual bind must not restore stale settings.
func TestBindingRechecksDisabledDeviceAfterElderLock(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	mustExec(t, pool, `INSERT INTO bind_code(code,old_id,expire_time,used)
		VALUES('RACE01',1,CURRENT_TIMESTAMP(6)+INTERVAL 1 DAY,0)`)
	ctx := context.Background()
	gate, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Rollback()
	var elderID int64
	if err := gate.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE").Scan(&elderID); err != nil {
		t.Fatal(err)
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- callAPI(h.deviceBind, map[string]any{
			"deviceNo": "WATCH001", "secret": testSecret, "code": "RACE01",
		})
	}()
	// The request can now authenticate but cannot pass the elder lock.
	// Both an early rejection and a post-lock rejection must fail closed.
	time.Sleep(80 * time.Millisecond)
	mustExec(t, pool, "UPDATE device SET status=0,alias='disabled-by-admin' WHERE device_id=1")
	if err := gate.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-result:
		if res.Code != http.StatusForbidden {
			t.Fatalf("bind accepted a disabled device: HTTP %d %s", res.Code, res.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bind remained blocked after releasing elder lock")
	}
	var status, used int
	var alias string
	if err := pool.QueryRowContext(ctx, "SELECT status,alias FROM device WHERE device_id=1").Scan(&status, &alias); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT used FROM bind_code WHERE code='RACE01'").Scan(&used); err != nil {
		t.Fatal(err)
	}
	if status != 0 || alias != "disabled-by-admin" || used != 0 {
		t.Fatalf("bind overwrote admin changes: status=%d alias=%q codeUsed=%d", status, alias, used)
	}
}
