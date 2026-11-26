package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

func TestHarmonyWatchNegotiationAndNativeRoundTrip(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	h := testHandler(t, pool)
	legacy := api(t, h.deviceLogin, map[string]any{"deviceNo": "WATCH001", "secret": testSecret})
	if _, added := legacy["protocolVersion"]; added {
		t.Fatal("legacy contract changed", legacy)
	}
	login := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "platform": "harmonyos", "protocolVersion": 1}
	reply := api(t, h.deviceLogin, login)
	if reply["protocolVersion"] != float64(1) || reply["maxBatchSize"] != float64(200) || reply["bindingVersion"] != float64(1) {
		t.Fatal(reply)
	}
	login["protocolVersion"] = 2
	if w := callAPI(h.deviceLogin, login); w.Code != 400 {
		t.Fatalf("unsupported version: %d %s", w.Code, w.Body.String())
	}
	login["protocolVersion"], login["secret"] = 1, "incorrect-secret"
	if out := apiFailure(t, 401, h.deviceLogin, login); out["returnCode"] == "1111" {
		t.Fatal("platform bypassed credentials")
	}
	at := time.Now().UTC().Truncate(time.Second)
	hr := health.Sample{RecordID: "native-hr", Source: "yihe-harmonyos", SourceDevice: "WATCH001", SourceVersion: "harmonyos-0.2.0", Metric: "heart_rate", Unit: "count/min", Value: health.Number(73), Start: at, End: at, Semantics: "instant", Version: 1, Metadata: map[string]string{"simulated": "false", "timestampBasis": "app-received", "sensorTimestamp": "1000000000"}}
	steps := hr
	steps.RecordID, steps.Metric, steps.Unit, steps.Value, steps.Semantics, steps.Start = "native-steps", "steps", "count", health.Number(5), "interval", at.Add(-time.Minute)
	batch := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "samples": []health.Sample{hr, steps}}
	if out := api(t, h.healthBatch, batch); out["accepted"] != float64(2) {
		t.Fatal(out)
	}
	if out := api(t, h.healthBatch, batch); out["duplicates"] != float64(2) {
		t.Fatal(out)
	}
	latest := api(t, latestHealthRoute(h, session.Principal{Role: auth.RoleAdmin, ID: 11}), map[string]any{"oldId": 1})
	raw, _ := json.Marshal(latest["items"])
	var records []health.Record
	if err := json.Unmarshal(raw, &records); err != nil || len(records) != 2 {
		t.Fatalf("%s %v", raw, err)
	}
	for _, record := range records {
		if record.Sample.Source != "yihe-harmonyos" || record.Sample.Metadata["timestampBasis"] != "app-received" || record.Sample.Metadata["simulated"] != "false" {
			t.Fatal(record)
		}
	}
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	if w := callAPI(h.healthBatch, batch); w.Code != 409 {
		t.Fatalf("stale native batch accepted: %d %s", w.Code, w.Body.String())
	}
}
