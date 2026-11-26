package handlers

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestPendingVitalsDetailHonorsEachHandledTime(t *testing.T) {
	h := extraAuditHandler(t)
	seedDevice(t, h.DB)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	mustExec(t, h.DB, "INSERT INTO ratepre(id,device_id,old_id,hr,bp,rate_state,pre_state,create_time) VALUES(1,1,1,150,180,'1','1','2026-10-20 10:00:00')")
	for _, handled := range []string{"rate", "pre"} {
		mustExec(t, h.DB, "UPDATE ratepre SET rate_handled_at=NULL,pre_handled_at=NULL WHERE id=1")
		mustExec(t, h.DB, "UPDATE ratepre SET "+handled+"_handled_at='2026-10-20 11:00:00' WHERE id=1")
		selected := api(t, withHealthPrincipal(h, admin, h.getVitalAlert), map[string]any{"recordId": 1})
		if selected[handled+"Bad"] != float64(0) || selected["hasAlert"] != float64(1) {
			t.Fatalf("record detail ignored handled time: %v", selected)
		}
		for _, kind := range []string{"", "rate", "pre"} {
			if kind == handled {
				continue
			}
			byOwner := api(t, withHealthPrincipal(h, admin, h.getVitalAlert), map[string]any{"oldId": 1, "kind": kind})
			if !reflect.DeepEqual(byOwner, selected) {
				t.Errorf("handled=%s kind=%q owner detail resurrected handled metric: got=%v want=%v", handled, kind, byOwner, selected)
			}
		}
	}
	mustExec(t, h.DB, "UPDATE ratepre SET rate_handled_at='2026-10-20 11:00:00',pre_handled_at='2026-10-20 11:00:00' WHERE id=1")
	if out := api(t, withHealthPrincipal(h, admin, h.getVitalAlert), map[string]any{"oldId": 1}); out["hasAlert"] != float64(0) {
		t.Fatalf("fully handled record remained pending: %v", out)
	}
	mustExec(t, h.DB, "UPDATE ratepre SET rate_handled_at='2026-10-20 09:00:00',pre_handled_at='2026-10-20 09:00:00' WHERE id=1")
	if out := api(t, withHealthPrincipal(h, admin, h.getVitalAlert), map[string]any{"oldId": 1}); out["rateBad"] != float64(1) || out["preBad"] != float64(1) {
		t.Fatalf("older handling suppressed newer observations: %v", out)
	}
}

func TestContinuousVitalsResolutionRejectsUnknownKind(t *testing.T) {
	h := extraAuditHandler(t)
	seedDevice(t, h.DB)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	mustExec(t, h.DB, "INSERT INTO ratepre(id,device_id,old_id,hr,bp,rate_state,pre_state,create_time) VALUES(1,1,1,150,180,'1','1','2026-10-20 10:00:00')")
	for _, kind := range []any{"rat", "all", 1, map[string]any{"kind": "rate"}} {
		mustExec(t, h.DB, "UPDATE ratepre SET rate_state='1',pre_state='1',rate_handled_at=NULL,pre_handled_at=NULL WHERE id=1")
		out := callAPI(withHealthPrincipal(h, admin, h.handleResolveVitalAlert), map[string]any{"recordId": 1, "kind": kind})
		if out.Code != 400 {
			t.Errorf("unknown kind %v accepted: HTTP %d %s", kind, out.Code, out.Body.String())
		}
		var rate, pre string
		var rateAt, preAt bool
		if err := h.DB.QueryRowContext(context.Background(), "SELECT rate_state,pre_state,rate_handled_at IS NULL,pre_handled_at IS NULL FROM ratepre WHERE id=1").Scan(&rate, &pre, &rateAt, &preAt); err != nil {
			t.Fatal(err)
		}
		if rate != "1" || pre != "1" || !rateAt || !preAt {
			t.Errorf("invalid kind %v cleared alerts: rate=%s pre=%s untouchedTimes=%v/%v", kind, rate, pre, rateAt, preAt)
		}
	}
	for _, kind := range []string{"rate", "pre"} {
		out := api(t, withHealthPrincipal(h, admin, h.handleResolveVitalAlert), map[string]any{"recordId": 1, "kind": kind})
		if out["returnCode"] != "1111" {
			t.Fatal(out)
		}
		if kind == "rate" {
			var pre string
			if err := h.DB.QueryRowContext(context.Background(), "SELECT pre_state FROM ratepre WHERE id=1").Scan(&pre); err != nil || pre != "1" {
				t.Fatalf("heart-rate resolution changed pressure: pre=%s err=%v", pre, err)
			}
		}
	}
	// The existing omitted/empty-kind operation intentionally handles both kinds.
	for _, body := range []map[string]any{{"recordId": 1}, {"recordId": 1, "kind": ""}} {
		mustExec(t, h.DB, "UPDATE ratepre SET rate_state='1',pre_state='1',rate_handled_at=NULL,pre_handled_at=NULL WHERE id=1")
		out := api(t, withHealthPrincipal(h, admin, h.handleResolveVitalAlert), body)
		var rate, pre string
		if err := h.DB.QueryRowContext(context.Background(), "SELECT rate_state,pre_state FROM ratepre WHERE id=1").Scan(&rate, &pre); err != nil || rate != "0" || pre != "0" || out["returnCode"] != "1111" {
			t.Fatalf("existing bulk operation changed: response=%v state=%s/%s err=%v", out, rate, pre, err)
		}
	}
}

func TestVitalAlertFamilyReadsFollowCurrentResidentOwnership(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001'),(2,'13800000002')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO ratepre(id,device_id,old_id,hr,rate_state,create_time) VALUES(1,1,1,150,'1',CURRENT_TIMESTAMP(6))")
	h := testHandler(t, pool)
	first := withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1}, h.getVitalAlert)
	other := withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 2, OldID: 1}, h.getVitalAlert)
	for _, input := range []map[string]any{{"oldId": 1}, {"recordId": 1}} {
		if got := callAPI(first, input); got.Code != 200 {
			t.Fatalf("authorized family denied: %d %s", got.Code, got.Body.String())
		}
		if got := callAPI(other, input); got.Code != http.StatusForbidden {
			t.Fatalf("other family accessed alert: %d %s", got.Code, got.Body.String())
		}
	}
	if got := callAPI(other, map[string]any{"recordId": 999}); got.Code != http.StatusForbidden {
		t.Fatalf("unknown alert ID disclosed existence: %d %s", got.Code, got.Body.String())
	}
	mustExec(t, pool, "UPDATE oldperson SET family_id=2 WHERE old_id=1")
	if got := callAPI(first, map[string]any{"recordId": 1}); got.Code != http.StatusForbidden {
		t.Fatalf("former family retained alert access: %d %s", got.Code, got.Body.String())
	}
	if got := callAPI(other, map[string]any{"recordId": 1}); got.Code != 200 {
		t.Fatalf("newly authorized family denied: %d %s", got.Code, got.Body.String())
	}
}

func TestContinuousVitalsResolutionReasonMatchesSelectedRecord(t *testing.T) {
	h := extraAuditHandler(t)
	seedDevice(t, h.DB)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	resolve := withHealthPrincipal(h, admin, h.deviceResolveAlert)
	detailRead := withHealthPrincipal(h, admin, h.deviceAlertDetail)
	mustExec(t, h.DB, "INSERT INTO ratepre(id,device_id,old_id,hr,bp,rate_state,pre_state,create_time) VALUES(1,1,1,150,180,'1','1','2026-10-20 10:00:00'),(2,1,1,40,70,'1','1','2026-10-20 11:00:00')")
	for _, tc := range []struct{ kind, reason, state string }{{"rate", "心率偏高", "rate_state"}, {"pre", "血压偏高", "pre_state"}} {
		out := api(t, resolve, map[string]any{"deviceId": 1, "recordId": 1, "kind": tc.kind})
		if out["returnCode"] != "1111" || out["handledReason"] != tc.reason {
			t.Errorf("%s selected high record but returned latest low record's reason: %v", tc.kind, out)
		}
		var selected, newer string
		if err := h.DB.QueryRowContext(context.Background(), "SELECT "+tc.state+" FROM ratepre WHERE id=1").Scan(&selected); err != nil {
			t.Fatal(err)
		}
		if err := h.DB.QueryRowContext(context.Background(), "SELECT "+tc.state+" FROM ratepre WHERE id=2").Scan(&newer); err != nil {
			t.Fatal(err)
		}
		if selected != "0" || newer != "1" {
			t.Errorf("wrong resolution target: selected=%s newer=%s", selected, newer)
		}
		detail := api(t, detailRead, map[string]any{"deviceId": 1, "kind": tc.kind})
		if detail["recordId"] != float64(2) || detail["hasAlert"] != float64(1) {
			t.Errorf("remaining latest alert lost after targeted resolution: %v", detail)
		}
		bulk := api(t, resolve, map[string]any{"deviceId": 1, "kind": tc.kind})
		if bulk["returnCode"] != "1111" || (tc.kind == "rate" && bulk["handledReason"] != "心率偏低") || (tc.kind == "pre" && bulk["handledReason"] != "血压偏低") {
			t.Errorf("existing latest/bulk resolution changed: %v", bulk)
		}
	}
}
