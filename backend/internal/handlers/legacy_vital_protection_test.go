package handlers

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
	"yihe-care/internal/vitalrepo"
)

func TestProtectedLegacyVitalsMySQL(t *testing.T) {
	h := historyLabelsHandler(t)
	ctx := context.Background()
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	family := session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}
	mustExec(t, h.DB, "INSERT INTO family(family_id,family_phone) VALUES(2,'13800000002')")
	mustExec(t, h.DB, "INSERT INTO ratepre(id,device_id,old_id,hr,bp,rate_state,pre_state,create_time) VALUES"+
		"(1,1,1,70,120,'0','0','2026-10-20 09:00:00'),(2,1,1,NULL,130,NULL,'0','2026-10-20 09:30:00')")
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "beTime": "2026-10-20 10:00:00", "hr": 150})

	var plain, protected int
	err := h.DB.QueryRowContext(ctx, "SELECT COUNT(hr)+COUNT(bp),COUNT(measurement_cipher) FROM ratepre WHERE id=3").Scan(&plain, &protected)
	if err != nil || plain != 0 || protected != 1 {
		t.Fatalf("report mixed protected and plaintext measurements: %d/%d, err=%v", plain, protected, err)
	}
	checkLatest := func(out map[string]any) {
		t.Helper()
		records := out["vitalRecords"].([]any)
		if len(records) != 1 {
			t.Fatalf("expected one latest measurement: %v", records)
		}
		row := records[0].(map[string]any)
		if row["hr"] != 150.0 || row["bp"] != nil || row["recordId"] != 3.0 {
			t.Fatalf("latest measurement changed: %v", row)
		}
	}
	checkLatest(api(t, withHealthPrincipal(h, admin, h.listLatestVitals), map[string]any{"page": 1, "pageSize": 10}))
	checkLatest(api(t, withHealthPrincipal(h, family, h.listVitalHistory), map[string]any{"oldId": 1}))

	chart := withHealthPrincipal(h, family, h.getVitalChart)
	chartArgs := map[string]any{"oldId": 1}
	before := api(t, chart, chartArgs)
	if !reflect.DeepEqual(before["hrs"], []any{70.0, nil, 150.0}) || !reflect.DeepEqual(before["bps"], []any{120.0, 130.0, nil}) {
		t.Fatalf("mixed chart lost measurements or nulls: %v", before)
	}
	deviceAlert := api(t, withHealthPrincipal(h, admin, h.deviceAlertDetail), map[string]any{"deviceId": 1, "kind": "rate"})
	if deviceAlert["value"] != 150.0 || deviceAlert["reason"] != "心率偏高" {
		t.Fatalf("device alert lost protected measurement: %v", deviceAlert)
	}
	detail := withHealthPrincipal(h, family, h.getVitalAlert)
	if out := api(t, detail, map[string]any{"recordId": 3}); out["hr"] != 150.0 || out["hasAlert"] != 1.0 {
		t.Fatalf("family alert lost protected measurement: %v", out)
	}
	other := session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 1}
	if got := callAPI(withHealthPrincipal(h, other, h.getVitalAlert), map[string]any{"recordId": 3}); got.Code != http.StatusForbidden {
		t.Fatalf("a different family received protected vital data: HTTP %d", got.Code)
	}

	scan, err := vitalrepo.BackfillMeasurements(ctx, h.DB.DB, h.Box, 1, false)
	if err != nil || scan.Before != 2 || scan.Migrated != 0 || scan.Remaining == nil || *scan.Remaining != 2 {
		t.Fatalf("read-only scan: %+v, err=%v", scan, err)
	}
	first, err := vitalrepo.BackfillMeasurements(ctx, h.DB.DB, h.Box, 1, true)
	if !errors.Is(err, vitalrepo.ErrMeasurementBackfillIncomplete) || first.Migrated != 1 || first.Remaining == nil || *first.Remaining != 1 {
		t.Fatalf("bounded application: %+v, err=%v", first, err)
	}
	for _, remaining := range []int{1, 0} {
		result, err := vitalrepo.BackfillMeasurements(ctx, h.DB.DB, h.Box, 1, true)
		if err != nil || result.Migrated != remaining || result.Remaining == nil || *result.Remaining != 0 {
			t.Fatalf("resume or idempotent run: %+v, err=%v", result, err)
		}
	}
	after := api(t, chart, chartArgs)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("chart changed after backfill: before=%v, after=%v", before, after)
	}

	box := h.Box
	h.Box = nil
	if got := callAPI(chart, chartArgs); got.Code != http.StatusInternalServerError {
		t.Errorf("missing reader produced chart: HTTP %d", got.Code)
	}
	h.Box = box
	if _, err := h.DB.ExecContext(ctx, "UPDATE ratepre SET hr=151 WHERE id=3"); err == nil {
		t.Fatal("database accepted simultaneous plaintext and ciphertext")
	}
	mustExec(t, h.DB, "UPDATE ratepre SET old_id=2 WHERE id=3")
	adminDetail := withHealthPrincipal(h, admin, h.getVitalAlert)
	if got := callAPI(adminDetail, map[string]any{"recordId": 3}); got.Code != http.StatusInternalServerError {
		t.Fatalf("changed owner was accepted: HTTP %d", got.Code)
	}
	mustExec(t, h.DB, "UPDATE ratepre SET old_id=1 WHERE id=3")
	var copied string
	if err := h.DB.QueryRowContext(ctx, "SELECT measurement_cipher FROM ratepre WHERE id=3").Scan(&copied); err != nil {
		t.Fatal(err)
	}
	mustExec(t, h.DB, "UPDATE ratepre SET measurement_cipher=? WHERE id=1", copied)
	if got := callAPI(chart, chartArgs); got.Code != http.StatusInternalServerError {
		t.Fatalf("copied cipher was accepted under another record: HTTP %d", got.Code)
	}
}
