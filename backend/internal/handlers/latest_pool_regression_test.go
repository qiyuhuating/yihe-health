package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestFamilyHealthReadsUseDedicatedPool(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "1")
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	p := session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}
	for _, item := range []struct {
		path, body string
		fn         http.HandlerFunc
	}{
		{"/api/v1/health/latest", `{}`, h.healthLatest},
		{"/api/v1/health/statistics", `{"source":"watch","sourceDevice":"WATCH001","metric":"heart_rate","semantics":"instant","from":"2026-10-20T00:00:00Z","to":"2026-10-21T00:00:00Z"}`, h.healthStatistics},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		req := httptest.NewRequest(http.MethodPost, item.path, bytes.NewBufferString(item.body)).WithContext(ctx)
		w := httptest.NewRecorder()
		withHealthPrincipal(h, p, item.fn)(w, req)
		cancel()
		if w.Code != http.StatusOK {
			t.Fatalf("%s blocked by ordinary pool: HTTP %d %s", item.path, w.Code, w.Body)
		}
	}
}

func TestLatestUsesSingleAuthorizedConnection(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := testHandler(t, pool)
	pool.SetMaxOpenConns(1)
	handler := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthLatest)
	body, err := json.Marshal(map[string]any{"oldId": 1})
	if err != nil {
		t.Fatal(err)
	}
	// Both cold backfill and warm projection must complete with one connection.
	for range 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/health/latest", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		handler(w, req)
		cancel()
		if w.Code != http.StatusOK {
			t.Fatalf("single connection HTTP %d %s", w.Code, w.Body)
		}
	}
}

func TestFamilyScopedReadsAvoidUnlockedLookup(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "0")
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	h := testHandler(t, pool)
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	selects := func() int64 {
		t.Helper()
		var name string
		var count int64
		if err := pool.QueryRowContext(t.Context(), "SHOW SESSION STATUS LIKE 'Com_select'").Scan(&name, &count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	p := session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}
	handlers := []http.HandlerFunc{h.healthChanges, h.healthBaseline, h.listVitalHistory, h.getVitalChart, h.listActivityHistory, h.getActivityChart}
	for _, handler := range handlers {
		before := selects()
		got := callAPI(withHealthPrincipal(h, p, handler), map[string]any{})
		after := selects()
		if got.Code != http.StatusOK || after-before != 2 {
			t.Fatalf("family scoped read: HTTP %d, SELECTs=%d, body=%s", got.Code, after-before, got.Body)
		}
	}
	mustExec(t, pool, "UPDATE oldperson SET family_id=NULL WHERE old_id=1")
	for _, handler := range handlers {
		before := selects()
		got := callAPI(withHealthPrincipal(h, p, handler), map[string]any{})
		after := selects()
		if got.Code != http.StatusForbidden || after-before != 1 {
			t.Fatalf("revoked family read: HTTP %d, SELECTs=%d", got.Code, after-before)
		}
	}
}
