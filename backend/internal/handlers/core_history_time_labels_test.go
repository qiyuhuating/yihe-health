package handlers

import (
	"reflect"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func historyLabelsHandler(t *testing.T) *Handler {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001')")
	mustExec(t, pool, "UPDATE oldperson SET device_id=1,family_id=1 WHERE old_id=1")
	return testHandler(t, pool)
}

func TestContinuousVitalHistoryLabelsKeepFullObservationTime(t *testing.T) {
	h := historyLabelsHandler(t)
	roles := []session.Principal{{Role: auth.RoleAdmin, ID: 11}, {Role: auth.RoleFamily, ID: 1, OldID: 1}}
	read := withHealthPrincipal(h, roles[0], h.getVitalChart)
	empty := api(t, read, map[string]any{"oldId": 1})
	if empty["time"] != "" || len(empty["createTimes"].([]any)) != 0 {
		t.Fatalf("empty history invented a time: %v", empty)
	}
	report := func(at string, fields map[string]any) {
		fields["deviceNo"], fields["secret"], fields["beTime"] = "WATCH001", testSecret, at
		api(t, h.deviceReport, fields)
	}
	check := func(times, hrs, bps []any, title string) {
		t.Helper()
		for _, principal := range roles {
			out := api(t, withHealthPrincipal(h, principal, h.getVitalChart), map[string]any{"oldId": 1})
			if !reflect.DeepEqual(out["createTimes"], times) || !reflect.DeepEqual(out["hrs"], hrs) || !reflect.DeepEqual(out["bps"], bps) || out["time"] != title {
				t.Errorf("role=%s chart times or metric alignment incorrect: %v", principal.Role, out)
			}
			if _, obsolete := out["ratepreDtoListDetial"]; obsolete {
				t.Error("unused duplicate history field returned")
			}
		}
	}
	report("2026-10-20 09:59:50", map[string]any{"hr": 70, "bp": 120})
	report("2026-10-20 10:00:10", map[string]any{"hr": 80})
	check([]any{"2026-10-20 09:59:50", "2026-10-20 10:00:10"}, []any{70.0, 80.0}, []any{120.0, nil}, "2026-10-20")
	report("2026-10-21 00:00:10", map[string]any{"bp": 130})
	report("2026-10-21 00:00:20", map[string]any{"hr": 90, "bp": 140})
	check([]any{"2026-10-20 09:59:50", "2026-10-20 10:00:10", "2026-10-21 00:00:10", "2026-10-21 00:00:20"}, []any{70.0, 80.0, nil, 90.0}, []any{120.0, nil, 130.0, 140.0}, "2026-10-20 至 2026-10-21")
}

func TestContinuousSleepHistoryLabelsKeepFullCalendarDate(t *testing.T) {
	h := historyLabelsHandler(t)
	for _, point := range []struct {
		at     string
		fields map[string]any
	}{{"2026-09-30 08:00:00", map[string]any{"sleep": 8}}, {"2026-10-01 08:00:00", map[string]any{"sleep": 7}}, {"2025-12-31 08:00:00", map[string]any{"sleep": 0}}, {"2026-01-01 08:00:00", map[string]any{"step": 100}}} {
		point.fields["deviceNo"], point.fields["secret"], point.fields["beTime"] = "WATCH001", testSecret, point.at
		api(t, h.deviceReport, point.fields)
	}
	want := []any{"2025-12-31", "2026-01-01", "2026-09-30", "2026-10-01"}
	for _, principal := range []session.Principal{{Role: auth.RoleAdmin, ID: 11}, {Role: auth.RoleFamily, ID: 1, OldID: 1}} {
		out := api(t, withHealthPrincipal(h, principal, h.getActivityChart), map[string]any{"oldId": 1})
		if !reflect.DeepEqual(out["createTime"], want) || !reflect.DeepEqual(out["sleep"], []any{0.0, nil, 8.0, 7.0}) {
			t.Errorf("role=%s sleep dates or values misaligned: %v", principal.Role, out)
		}
		for i, row := range out["activityRecords"].([]any) {
			if row.(map[string]any)["createTime"] != want[i] {
				t.Errorf("sleep detail date differs from chart label: %v", row)
			}
		}
	}
}
