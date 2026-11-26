package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/devicerepo"
)

func TestSOSRejectsStaleBinding(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'new owner')")
	mustExec(t, pool, "UPDATE device SET old_id=2, health_epoch=health_epoch+1 WHERE device_id=1")
	response := callAPI(h.deviceSos, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret,
		"requestId": "stale-binding", "bindingVersion": 1,
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("old binding SOS: HTTP %d, want 409: %s", response.Code, response.Body.String())
	}
	var incidents, alarms int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM care_incident").Scan(&incidents); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM alarm_precess").Scan(&alarms); err != nil {
		t.Fatal(err)
	}
	if incidents != 0 || alarms != 0 {
		t.Fatalf("stale request created incidents=%d alarms=%d", incidents, alarms)
	}
	response = callAPI(h.deviceSos, map[string]any{
		"deviceNo": "WATCH001", "secret": testSecret, "requestId": "missing-version-after-rebind",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unversioned SOS after rebind: HTTP %d, want 400", response.Code)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM care_incident").Scan(&incidents); err != nil || incidents != 0 {
		t.Fatalf("unversioned request created incidents=%d err=%v", incidents, err)
	}
}

func TestSOSConcurrentRebind(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'new owner')")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	h := testHandler(t, pool)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- callAPI(h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "racing-rebind", "bindingVersion": 1})
	}()
	waits := `SELECT count(*) FROM performance_schema.data_lock_waits w
		JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID AND l.ENGINE=w.ENGINE
		WHERE l.OBJECT_SCHEMA=DATABASE() AND l.OBJECT_NAME='oldperson'`
	for {
		var n int
		if err := pool.QueryRowContext(ctx, waits).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("SOS did not wait on the owner lock")
		default:
			time.Sleep(10 * time.Millisecond)
		}
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
			t.Fatalf("racing SOS: HTTP %d: %s", response.Code, response.Body.String())
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_incident").Scan(&n); err != nil || n != 0 {
		t.Fatalf("incidents=%d err=%v", n, err)
	}
}

func TestSOSVersionedRetryAndRequiredIdentity(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	payload := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "versioned-retry", "bindingVersion": 1}
	first := api(t, h.deviceSos, payload)
	again := api(t, h.deviceSos, payload)
	if first["incidentId"] != again["incidentId"] || again["duplicate"] != true {
		t.Fatalf("retry: first=%v again=%v", first, again)
	}
	for _, invalid := range []map[string]any{
		{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "missing-version"},
		{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1},
	} {
		response := callAPI(h.deviceSos, invalid)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("missing identity: HTTP %d, want 400: %s", response.Code, response.Body.String())
		}
	}
	for _, invalid := range []any{nil, 0, -1, 1.5, "bad"} {
		response := callAPI(h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": invalid, "requestId": "invalid-version"})
		if response.Code != http.StatusBadRequest {
			t.Fatalf("bindingVersion %v: HTTP %d", invalid, response.Code)
		}
	}
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'new owner')")
	mustExec(t, pool, "UPDATE device SET old_id=2 WHERE device_id=1")
	payload["bindingVersion"] = 2
	response := callAPI(h.deviceSos, payload)
	if response.Code != http.StatusConflict {
		t.Fatalf("request ID reused by new owner: HTTP %d %s", response.Code, response.Body.String())
	}
}

func TestSOSConcurrentDuplicateRequests(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	payload := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "concurrent-retry", "bindingVersion": 1}
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for range 8 {
		go func() { <-start; results <- callAPI(h.deviceSos, payload) }()
	}
	close(start)
	var incidentID float64
	duplicates := 0
	for range 8 {
		response := <-results
		if response.Code != http.StatusOK {
			t.Fatalf("concurrent retry: HTTP %d %s", response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		id, ok := body["incidentId"].(float64)
		if !ok || body["returnCode"] != "1111" || (incidentID != 0 && incidentID != id) {
			t.Fatalf("invalid retry response: %v", body)
		}
		incidentID = id
		if body["duplicate"] == true {
			duplicates++
		}
	}
	if duplicates != 7 {
		t.Fatalf("duplicate acknowledgements=%d, want 7", duplicates)
	}
	var incidents, alarms int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM care_incident").Scan(&incidents); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM alarm_precess").Scan(&alarms); err != nil {
		t.Fatal(err)
	}
	if incidents != 1 || alarms != 1 {
		t.Fatalf("incidents=%d alarms=%d", incidents, alarms)
	}
}

func TestSOSPersistenceRollbackLeavesNoPartialRows(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	ctx := context.Background()

	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	d, err := devicerepo.ScanDeviceRecord(devicerepo.LockedDeviceRow(ctx, tx, 1))
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if _, err = (telemetryRepository{}).PersistSOSIncident(ctx, tx, d, "", "", "rollback-probe", positionMetadata{Version: 1, Validity: "missing"}, time.Now()); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"alarm_precess", "care_incident", "care_incident_event"} {
		var count int
		if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s retained %d rows after rollback", table, count)
		}
	}
}
