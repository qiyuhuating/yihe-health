package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/config"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

func TestHealthStatisticsHotCacheConsistency(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	writer := testHandler(t, pool) // A second server has no access to h's cache.
	seedDevice(t, pool)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "hot", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	batch := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}}
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": at.Add(-time.Hour), "to": at.Add(time.Hour)}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	check := func(count int, mean float64) {
		t.Helper()
		for range 2 { // Exercise the first read and the warm response.
			out := api(t, admin, query)["statistics"].(map[string]any)
			if out["count"] != float64(count) || (count > 0 && out["mean"] != mean) {
				t.Fatalf("unexpected statistics: %v", out)
			}
		}
	}
	check(0, 0) // A cached empty range must invalidate on the first write.
	api(t, writer.healthBatch, batch)
	check(1, 72)
	s.Version, s.Value = 2, health.Number(91)
	batch["samples"] = []health.Sample{s}
	api(t, writer.healthBatch, batch)
	check(1, 91)
	// A rolled-back correction must not invalidate the committed result.
	s.Version, s.Value = 3, health.Number(120)
	tx, err := pool.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := int64(1)
	prepared, err := health.PrepareBatch([]health.Sample{s})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = prepared.SaveProtected(context.Background(), tx, health.Owner{OldID: 1, DeviceID: &deviceID, Epoch: 1}, h.Box); err != nil {
		t.Fatal(err)
	}
	check(1, 91)
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	check(1, 91)
	s.Deleted = true
	batch["samples"] = []health.Sample{s}
	api(t, writer.healthBatch, batch)
	check(0, 0)
	// Restarting a reader repopulates from durable state.
	restarted := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(restarted.Sessions.Close)
	admin2 := withHealthPrincipal(restarted, session.Principal{Role: auth.RoleAdmin, ID: 11}, restarted.healthStatistics)
	if got := api(t, admin2, query)["statistics"].(map[string]any)["count"]; got != float64(0) {
		t.Fatal("restart returned deleted sample", got)
	}
}

func TestHealthStatisticsHotCacheAuthorizationAndIdentity(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'1'),(2,'2')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,family_id) VALUES(2,'other',2)")
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "hot", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}})
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": at.Add(-time.Hour), "to": at.Add(time.Hour)}
	family := withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 1}, h.healthStatistics)
	api(t, family, query)
	api(t, family, query)
	mustExec(t, pool, "UPDATE oldperson SET family_id=2 WHERE old_id=1")
	if got := callAPI(family, query); got.Code != http.StatusForbidden {
		t.Fatal("warm cache bypassed changed family authorization", got.Code)
	}
	if got := callAPI(h.healthStatistics, query); got.Code != http.StatusUnauthorized {
		t.Fatal("warm cache bypassed authentication", got.Code)
	}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	for _, change := range []struct {
		field string
		value any
	}{
		{"oldId", int64(2)}, {"source", "other"}, {"sourceDevice", "other"}, {"metric", "body_mass"}, {"semantics", "interval"}, {"from", at.Add(time.Minute)}, {"to", at.Add(-time.Minute)},
	} {
		copyQuery := map[string]any{}
		for k, v := range query {
			copyQuery[k] = v
		}
		copyQuery[change.field] = change.value
		if out := api(t, admin, copyQuery)["statistics"].(map[string]any); out["count"] != float64(0) {
			t.Fatalf("cache identity omitted %s: %v", change.field, out)
		}
	}
	// Disabled and enabled paths preserve the same response contract.
	uncached := testHandler(t, pool)
	plain := withHealthPrincipal(uncached, session.Principal{Role: auth.RoleAdmin, ID: 11}, uncached.healthStatistics)
	if a, b := callAPI(admin, query), callAPI(plain, query); !bytes.Equal(a.Body.Bytes(), b.Body.Bytes()) {
		t.Fatalf("cache changed the response: %s / %s", a.Body.String(), b.Body.String())
	}
}

func TestHealthStatisticsHotCacheRequiresDatabaseValidation(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(1,'hot')")
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": "2026-10-22T00:00:00Z", "to": "2026-10-23T00:00:00Z"}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	api(t, admin, query)
	if len(h.healthCache.entries) != 0 {
		t.Fatal("cached a result without a committed revision")
	}
	mustExec(t, pool, "INSERT INTO health_owner_clock(old_id,revision,range_end) VALUES(1,1,1)")
	api(t, admin, query)
	if len(h.healthCache.entries) != 1 {
		t.Fatal("expected a warm entry before disconnecting MySQL")
	}
	pool.Close()
	if got := callAPI(admin, query); got.Code != http.StatusInternalServerError {
		t.Fatal("warm cache served without database revision validation", got.Code)
	}
}

func TestHealthStatisticsHotCacheReportingOffset(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(1,'hot')")
	// This observation falls on Oct 22 in UTC+8, and Oct 21 in UTC. Requests
	// for the same calendar date must not share a result across these offsets.
	at := time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "daily", Source: "watch", SourceDevice: "WATCH001", Metric: "steps", Semantics: "daily_total", Unit: "count", Value: health.Number(500), Start: at, End: at, Version: 1}
	tx, err := pool.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err = health.Save(context.Background(), tx, health.Owner{OldID: 1}, s); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	for i, offset := range []int{0, 8 * 3600} {
		at := time.Date(2026, 10, 22, 0, 0, 0, 0, time.FixedZone("", offset))
		query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "steps", "semantics": "daily_total", "from": at, "to": at.AddDate(0, 0, 1)}
		for range 2 {
			out := api(t, admin, query)
			if out["statistics"].(map[string]any)["count"] != float64(i) {
				t.Fatal("reporting offset merged daily windows", out)
			}
		}
	}
}

func TestHealthStatisticsHotCacheUnencodableTime(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	seedDevice(t, pool)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "offset", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}})
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	// The time parser accepts this offset on supported Go versions, whereas
	// Time.MarshalJSON rejects it. A failed key encoding must not hash nil.
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": "2026-10-23T00:00:00+24:00", "to": "2026-10-24T00:00:00+24:00"}
	if out := api(t, admin, query)["statistics"].(map[string]any); out["count"] != float64(1) {
		t.Fatal(out)
	}
	query["source"] = "other"
	if out := api(t, admin, query)["statistics"].(map[string]any); out["count"] != float64(0) {
		t.Fatal("failed key encoding mixed query identities", out)
	}
}

func TestHealthStatisticsHotCacheOverflowResultConsistency(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: 1 << 20, DataKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))})
	t.Cleanup(h.Sessions.Close)
	seedDevice(t, pool)
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "overflow-1", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(1e308), Start: at, End: at, Version: 1}
	other := s
	other.RecordID = "overflow-2"
	api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s, other}})
	query := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "from": at.Add(-time.Hour), "to": at.Add(time.Hour)}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	uncached := testHandler(t, pool)
	plain := withHealthPrincipal(uncached, session.Principal{Role: auth.RoleAdmin, ID: 11}, uncached.healthStatistics)
	// Previously overflowing finite sums now have a representable mean.
	// The cache must return the same result as the uncached aggregation.
	want := callAPI(plain, query)
	for range 2 {
		got := callAPI(admin, query)
		if got.Code != want.Code || !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
			t.Fatalf("cache changed an unencodable result: %s / %s", got.Body.String(), want.Body.String())
		}
	}
}
