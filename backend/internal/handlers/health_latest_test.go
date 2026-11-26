package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

func latestHealthRoute(h *Handler, principal session.Principal) http.HandlerFunc {
	mux := http.NewServeMux()
	h.routeHealth(mux)
	return withHealthPrincipal(h, principal, func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = "/api/v1/health/latest"
		mux.ServeHTTP(w, r)
	})
}

func TestHealthLatestPreservesSourcesPrecisionAndVersions(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := testHandler(t, pool)
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	a := health.Sample{RecordID: "newer", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Start: at.Add(900 * time.Nanosecond), End: at.Add(900 * time.Nanosecond), Version: 1, Value: health.Number(72)}
	b, c, upper, other, missing, interval, daily, sleep := a, a, a, a, a, a, a, a
	b.RecordID, b.Start, b.End, b.Value = "older", at.Add(100*time.Nanosecond), at.Add(100*time.Nanosecond), health.Number(60)
	c.RecordID, c.Value = "same-instant", health.Number(75)
	upper.Source, upper.Value = "WATCH", health.Number(91)
	other.SourceDevice, other.Value = "A ", health.Number(92)
	missing.RecordID, missing.Start, missing.End, missing.Deleted = "deleted", at.Add(time.Hour), at.Add(time.Hour), true
	interval.RecordID, interval.Metric, interval.Unit, interval.Semantics, interval.End, interval.Value = "steps-interval", "steps", "count", "interval", at.Add(time.Minute), health.Number(0)
	daily.RecordID, daily.Metric, daily.Unit, daily.Semantics, daily.Value = "steps-daily", "steps", "count", "daily_total", health.Number(10)
	sleep.RecordID, sleep.Metric, sleep.Unit, sleep.Semantics, sleep.Stage, sleep.End = "sleep", "sleep_stage", "min", "interval", "deep", sleep.Start.Add(time.Hour)
	deviceID := int64(1)
	save := func(samples ...health.Sample) {
		t.Helper()
		batch, err := health.PrepareBatch(samples)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, _, err = batch.SaveProtected(context.Background(), tx, health.Owner{OldID: 1, DeviceID: &deviceID, Epoch: 1}, h.Box); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	save(a, b, c, upper, other, missing, interval, daily, sleep)
	read := latestHealthRoute(h, session.Principal{Role: auth.RoleAdmin, ID: 11})
	check := func(want float64) {
		t.Helper()
		out := api(t, read, map[string]any{"oldId": 1})
		encoded, _ := json.Marshal(out["items"])
		var records []health.Record
		if err := json.Unmarshal(encoded, &records); err != nil || len(records) != 6 {
			t.Fatalf("source/semantics series lost: %s err=%v", encoded, err)
		}
		found := false
		wantValues := map[string]float64{"WATCH/A/heart_rate/instant": 91, "watch/A /heart_rate/instant": 92, "watch/A/heart_rate/instant": want, "watch/A/steps/interval": 0, "watch/A/steps/daily_total": 10, "watch/A/sleep_stage/interval": 60}
		for _, record := range records {
			if record.OldID != 1 || record.Epoch != 1 || record.DeviceID == nil || *record.DeviceID != 1 || record.Revision < 1 || record.SampleID == "" || record.Sample.Deleted {
				t.Fatalf("provenance lost: %+v", record)
			}
			s := record.Sample
			key := s.Source + "/" + s.SourceDevice + "/" + s.Metric + "/" + s.Semantics
			value, exists := wantValues[key]
			if !exists || s.Value == nil || *s.Value != value {
				t.Errorf("wrong latest value for source/semantics: %+v", s)
			}
			delete(wantValues, key)
			if s.Semantics == "interval" && !s.End.After(s.Start) {
				t.Errorf("interval endpoints lost: %+v", s)
			}
			if record.Sample.Source == "watch" && record.Sample.SourceDevice == "A" && record.Sample.Metric == "heart_rate" {
				found = true
				if record.Sample.Value == nil || *record.Sample.Value != want || !record.Sample.Start.Equal(a.Start) {
					t.Errorf("arrival order or microsecond index displaced latest sample: %+v want=%v", record.Sample, want)
				}
			}
		}
		if !found {
			t.Fatal("latest heart-rate series missing")
		}
		if len(wantValues) != 0 {
			t.Errorf("series missing: %v", wantValues)
		}
	}
	check(75)
	c.Version, c.Deleted = 2, true
	save(c)
	check(72)
	a.Version, a.Value = 2, health.Number(74)
	save(a)
	check(74)
}

func TestHealthLatestUsesCurrentFamilyOwnership(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001'),(2,'13800000002')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	read := latestHealthRoute(h, session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1})
	if items := api(t, read, map[string]any{})["items"].([]any); len(items) != 0 {
		t.Fatal(items)
	}
	other := latestHealthRoute(h, session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 1})
	if out := callAPI(other, map[string]any{"oldId": 1}); out.Code != 403 {
		t.Fatalf("other family accepted: HTTP %d %s", out.Code, out.Body.String())
	}
	mustExec(t, pool, "UPDATE oldperson SET family_id=2 WHERE old_id=1")
	if out := callAPI(read, map[string]any{}); out.Code != 403 {
		t.Fatalf("stale family ownership accepted: HTTP %d %s", out.Code, out.Body.String())
	}
}
