package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

func withHealthPrincipal(h *Handler, p session.Principal, f http.HandlerFunc) http.HandlerFunc {
	s := h.Sessions.Create(p)
	return func(w http.ResponseWriter, r *http.Request) { f(w, session.WithContext(r, s)) }
}

func TestHealthVersionsCursorAndIsolation(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	tm := time.Date(2026, 10, 18, 9, 0, 0, 0, time.FixedZone("", 8*3600))
	s := health.Sample{RecordID: "point-1", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: tm, End: tm, Version: 1}
	req := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}}
	api(t, h.healthBatch, req)
	out := api(t, h.healthBatch, req)
	if out["duplicates"] != float64(1) {
		t.Fatal(out)
	}
	s.Value = health.Number(73)
	req["samples"] = []health.Sample{s}
	if w := callAPI(h.healthBatch, req); w.Code != 409 {
		t.Fatalf("same-version conflict %d", w.Code)
	}
	s.Version = 2
	req["samples"] = []health.Sample{s}
	api(t, h.healthBatch, req)
	s.Version = 3
	s.Deleted = true
	req["samples"] = []health.Sample{s}
	api(t, h.healthBatch, req)
	admin := withHealthPrincipal(h, session.Principal{Role: "ADMIN", ID: 11}, h.healthChanges)
	var cursor float64
	for i := 1; i <= 3; i++ {
		out = api(t, admin, map[string]any{"oldId": 1, "after": cursor, "limit": 1})
		changes := out["changes"].([]any)
		if len(changes) != 1 {
			t.Fatal(out)
		}
		b, _ := json.Marshal(changes[0])
		var event health.Record
		json.Unmarshal(b, &event)
		if event.Sample.Version != int64(i) || event.Sample.Deleted != (i == 3) {
			t.Fatal(event)
		}
		if out["nextCursor"].(float64) <= cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = out["nextCursor"].(float64)
		if out["hasMore"] != (i < 3) {
			t.Fatal(out)
		}
	}
	out = api(t, admin, map[string]any{"oldId": 1, "after": cursor})
	if len(out["changes"].([]any)) != 0 || out["nextCursor"] != cursor {
		t.Fatal(out)
	}
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'1'),(2,'2')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	other := withHealthPrincipal(h, session.Principal{Role: "FAMILY", ID: 2}, h.healthChanges)
	if w := callAPI(other, map[string]any{"oldId": 1}); w.Code != 403 {
		t.Fatalf("family leak %d", w.Code)
	}
	// Any conflict rolls back all earlier samples in the same batch.
	newSample := s
	newSample.RecordID = "should-rollback"
	newSample.Version = 1
	newSample.Deleted = false
	s.Value = health.Number(100)
	req["samples"] = []health.Sample{newSample, s}
	if w := callAPI(h.healthBatch, req); w.Code != 409 {
		t.Fatal(w.Code)
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM health_sample").Scan(&count); err != nil || count != 1 {
		t.Fatalf("partial batch committed: %d %v", count, err)
	}
}

func TestHealthWriteCursorFollowsCommitOrder(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	ctx := context.Background()
	tm := time.Now().UTC()
	s := health.Sample{RecordID: "first", Source: "test", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: tm, End: tm, Version: 1}
	tx1, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback()
	first, _, err := health.Save(ctx, tx1, health.Owner{OldID: 1}, s)
	if err != nil {
		t.Fatal(err)
	}
	// An uncommitted earlier writer must prevent a later revision from becoming visible.
	tx2, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	s.RecordID = "second"
	if _, _, err = health.Save(blocked, tx2, health.Owner{OldID: 1}, s); err == nil {
		t.Fatal("later revision published before first transaction committed")
	}
	tx2.Rollback()
	var count int
	if err = pool.QueryRowContext(ctx, "SELECT count(*) FROM health_change").Scan(&count); err != nil || count != 0 {
		t.Fatalf("uncommitted change visible: %d %v", count, err)
	}
	if err = tx1.Commit(); err != nil {
		t.Fatal(err)
	}
	tx3, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx3.Rollback()
	second, _, err := health.Save(ctx, tx3, health.Owner{OldID: 1}, s)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx3.Commit(); err != nil {
		t.Fatal(err)
	}
	if second <= first {
		t.Fatalf("commit order reversed: %d %d", first, second)
	}
}

func TestHealthRoutesKeepAuthenticationAndCSRF(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	mux := http.NewServeMux()
	h.routeHealth(mux)
	app := csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, mux))
	for _, path := range []string{"changes", "statistics", "latest"} {
		r := httptest.NewRequest("POST", "/api/v1/health/"+path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("missing CSRF accepted for %s: %d", path, w.Code)
		}
	}
	tm := time.Now().UTC()
	s := health.Sample{RecordID: "hardware", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: tm, End: tm, Version: 1}
	b, _ := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}})
	r := httptest.NewRequest("POST", "/api/v1/device/healthBatch", strings.NewReader(string(b)))
	w := httptest.NewRecorder()
	app.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"1111"`) {
		t.Fatalf("authenticated hardware blocked by browser middleware: %d %s", w.Code, w.Body.String())
	}
}

func TestHealthBindingEpochAndStatistics(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	at := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "point", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	req := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}}
	api(t, h.healthBatch, req)
	admin := withHealthPrincipal(h, session.Principal{Role: "ADMIN", ID: 11}, h.healthStatistics)
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": at.Add(-time.Hour), "to": at.Add(time.Hour)}
	out := api(t, admin, query)
	if out["statistics"].(map[string]any)["mean"] != float64(72) {
		t.Fatal(out)
	}
	mustExec(t, pool, "UPDATE device SET alias='renamed' WHERE device_id=1")
	if out := api(t, h.deviceLogin, req); out["bindingVersion"] != float64(1) {
		t.Fatalf("ordinary device update changed epoch: %v", out)
	}
	// Even an unbind/rebind to the same person invalidates the earlier batch.
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	mustExec(t, pool, "UPDATE device SET old_id=1 WHERE device_id=1")
	if w := callAPI(h.healthBatch, req); w.Code != 409 {
		t.Fatalf("stale binding accepted: %d %s", w.Code, w.Body.String())
	}
	req["bindingVersion"] = 3
	api(t, h.healthBatch, req)
	if out := api(t, h.deviceHeartbeat, req); out["bindingVersion"] != float64(3) {
		t.Fatal(out)
	}
	var count int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM health_sample WHERE old_id=1").Scan(&count); err != nil || count != 2 {
		t.Fatalf("binding generations merged: %d %v", count, err)
	}
}

func TestHealthBadSecretDoesNotWaitForClock(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT revision FROM health_clock WHERE id=1 FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	s := health.Sample{RecordID: "point", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	b, _ := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": "incorrect", "bindingVersion": 1, "samples": []health.Sample{s}})
	r := httptest.NewRequest("POST", "/api/v1/device/healthBatch", strings.NewReader(string(b))).WithContext(ctx)
	w := httptest.NewRecorder()
	h.healthBatch(w, r)
	if ctx.Err() != nil || w.Code != 401 || !strings.Contains(w.Body.String(), `"code":"DEVICE_AUTH_FAILED"`) {
		t.Fatalf("unauthenticated request waited for write lock: %s", w.Body.String())
	}
}
