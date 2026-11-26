package handlers

import (
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
	"yihe-care/internal/vitalrepo"
)

func TestContinuousZeroBatteryAlert(t *testing.T) {
	now := time.Now()
	id := int64(1)
	low, high := 55, 110
	pr := vitalrepo.DeviceAlertState{DeviceID: 1, ResidentID: &id, LastSeen: &now, HRHigh: &high, HRLow: &low, BPHigh: &high, BPLow: &low}
	for _, battery := range []int{-1, 0, 19, 20} {
		pr.Battery = &battery
		if battery == -1 {
			pr.Battery = nil
		}
		alert := describePersonAlert(&pr, now)
		want := battery == 0 || battery == 19
		if (alert != nil) != want {
			t.Errorf("battery=%d alert=%v wantAlert=%v", battery, alert, want)
		}
	}
}

func TestContinuousConfiguredThresholdExplanation(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO watch_info(device_id,hrf,hrl,bpf,bpl) VALUES(1,75,55,200,180)")
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "hr": 80, "bp": 170})
	for _, body := range []map[string]any{{"oldId": 1}, {"recordId": 1}} {
		out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.getVitalAlert), body)
		reason, _ := out["reason"].(string)
		detail, _ := out["detail"].(string)
		if !strings.Contains(reason, "心率偏高") || !strings.Contains(reason, "血压偏低") || !strings.Contains(detail, "55-75") || !strings.Contains(detail, "180-200") {
			t.Errorf("configured thresholds ignored: %v", out)
		}
	}
}

func TestContinuousStatisticsExactSource(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	at := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	s := health.Sample{RecordID: "exact", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(72), Start: at, End: at, Version: 1}
	out := api(t, h.healthBatch, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{s}})
	if out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.healthStatistics)
	for _, tc := range []struct {
		source, device string
		count          float64
	}{{"watch", "WATCH001", 1}, {"watch ", "WATCH001", 0}, {"watch", "WATCH001 ", 0}, {"WATCH", "WATCH001", 0}} {
		result := api(t, admin, map[string]any{"oldId": 1, "source": tc.source, "sourceDevice": tc.device, "metric": "heart_rate", "semantics": "instant", "from": at.Add(-time.Hour), "to": at.Add(time.Hour)})["statistics"].(map[string]any)
		if result["count"] != tc.count {
			t.Errorf("source=%q device=%q statistics=%v want count=%v", tc.source, tc.device, result, tc.count)
		}
	}
}
