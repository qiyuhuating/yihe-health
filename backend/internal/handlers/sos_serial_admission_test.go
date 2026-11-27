package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSOSDuplicateWaitsOutsideTransaction(t *testing.T) {
	a := newHealthWriteAdmission()
	release, err := a.prioritizeSOS(t.Context(), "same", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		second, err := a.prioritizeSOS(ctx, "same", 1)
		if second != nil {
			second()
		}
		done <- err
	}()
	other, err := a.prioritizeSOS(t.Context(), "other", 2)
	if err != nil {
		t.Fatal(err)
	}
	other()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("duplicate acquired transaction admission while first SOS active: %v", err)
	}
	// Cancellation must not remove the first SOS's exclusion.
	ctx2, cancel2 := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel2()
	if next, err := a.prioritizeSOS(ctx2, "same", 1); next != nil || !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			next()
		}
		t.Fatal("canceled waiter released active SOS", err)
	}
}

func TestSOSRetriesLeaveReservedConnectionForOtherDevice(t *testing.T) {
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'other')")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT 2,'WATCH002',secret,2 FROM device WHERE device_id=1")
	h := testHandler(t, pool)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	lock, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var id int64
	if err := lock.QueryRowContext(ctx, "SELECT device_id FROM device WHERE device_id=1 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	call := func(device, requestID string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"deviceNo": device, "secret": testSecret, "requestId": requestID, "bindingVersion": 1})
		w := httptest.NewRecorder()
		h.deviceSos(w, httptest.NewRequest(http.MethodPost, "/api/v1/device/sos", bytes.NewReader(body)).WithContext(ctx))
		return w
	}
	first, second := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- call("WATCH001", "blocked-duplicate") }()
	waitFor := func(count int) {
		t.Helper()
		for {
			h.healthWrites.mu.Lock()
			n := h.healthWrites.urgent["WATCH001"]
			h.healthWrites.mu.Unlock()
			if n == count && pool.SOSPool().Stats().InUse == 1 {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("SOS did not reach expected local admission")
			case <-time.After(time.Millisecond):
			}
		}
	}
	waitFor(1)
	go func() { second <- call("WATCH001", "blocked-duplicate") }()
	waitFor(2)
	other := make(chan *httptest.ResponseRecorder, 1)
	start := time.Now()
	go func() { other <- call("WATCH002", "unrelated-emergency") }()
	select {
	case w := <-other:
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "1111") {
			t.Fatalf("unrelated SOS: %d %s", w.Code, w.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("duplicate requests exhausted emergency connections")
	}
	t.Logf("unrelated SOS completed while duplicate device locked: %s", time.Since(start))
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	var incident any
	for _, ch := range []chan *httptest.ResponseRecorder{first, second} {
		select {
		case w := <-ch:
			var result map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result["returnCode"] != "1111" {
				t.Fatal(w.Code, w.Body, err)
			}
			if incident != nil && incident != result["incidentId"] {
				t.Fatal("retry created different incident", result)
			}
			incident = result["incidentId"]
		case <-ctx.Done():
			t.Fatal("blocked SOS did not complete")
		}
	}
	var n int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM alarm_precess").Scan(&n); err != nil || n != 2 {
		t.Fatal("durable incident count", n, err)
	}
}

func TestSOSResidentConflictLeavesEmergencyConnection(t *testing.T) {
	t.Setenv("YIHE_SOS_PRIORITY", "1")
	t.Setenv("MYSQL_SOS_MAX_CONNS", "2")
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'other')")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT 2,'WATCH002',secret,1 FROM device WHERE device_id=1")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) SELECT 3,'WATCH003',secret,2 FROM device WHERE device_id=1")
	h := testHandler(t, pool)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	lock, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var id int64
	if err = lock.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	call := func(device string) *httptest.ResponseRecorder {
		data, _ := json.Marshal(map[string]any{"deviceNo": device, "secret": testSecret, "bindingVersion": 1, "requestId": "resident-conflict"})
		w := httptest.NewRecorder()
		h.deviceSos(w, httptest.NewRequest(http.MethodPost, "/api/v1/device/sos", bytes.NewReader(data)).WithContext(ctx))
		return w
	}
	waitPending := func(device string) {
		t.Helper()
		for {
			h.healthWrites.mu.Lock()
			pending := h.healthWrites.urgent[device] > 0
			h.healthWrites.mu.Unlock()
			if pending {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("SOS did not reach admission")
			case <-time.After(time.Millisecond):
			}
		}
	}
	first, second, other := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- call("WATCH001") }()
	waitPending("WATCH001")
	for pool.SOSPool().Stats().InUse != 1 {
		select {
		case <-ctx.Done():
			t.Fatal("first SOS did not start transaction")
		case <-time.After(time.Millisecond):
		}
	}
	go func() { second <- call("WATCH002") }()
	waitPending("WATCH002")
	start := time.Now()
	go func() { other <- call("WATCH003") }()
	select {
	case w := <-other:
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "1111") {
			t.Fatal(w.Code, w.Body)
		}
	case <-time.After(time.Second):
		t.Fatal("same resident SOS requests consumed both emergency connections")
	}
	t.Logf("unrelated resident SOS committed: %s", time.Since(start))
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []chan *httptest.ResponseRecorder{first, second} {
		select {
		case w := <-ch:
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "1111") {
				t.Fatal(w.Code, w.Body)
			}
		case <-ctx.Done():
			t.Fatal("queued SOS did not complete")
		}
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM alarm_precess").Scan(&count); err != nil || count != 3 {
		t.Fatal("durable alarms", count, err)
	}
}
