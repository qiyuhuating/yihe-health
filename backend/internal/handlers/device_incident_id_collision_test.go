package handlers

import (
	"context"
	"net/http"
	"testing"
)

func TestDeviceRequestIDCannotAliasDifferentIncidentKinds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		firstIsFall bool
	}{
		{name: "fall before SOS", firstIsFall: true},
		{name: "SOS before fall", firstIsFall: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testDatabase(t)
			seedDevice(t, pool)
			h := testHandler(t, pool)
			requestID := "cross-kind-collision-001"
			fall := map[string]any{
				"deviceNo": "WATCH001", "secret": testSecret,
				"bindingVersion": 1, "requestId": requestID, "alarm": "fall",
			}
			sos := map[string]any{
				"deviceNo": "WATCH001", "secret": testSecret,
				"bindingVersion": 1, "requestId": requestID,
			}
			if tc.firstIsFall {
				out := api(t, h.deviceReport, fall)
				if out["incidentState"] != "received" {
					t.Fatalf("initial fall was not persisted: %#v", out)
				}
				rejected := callAPI(h.deviceSos, sos)
				if rejected.Code != http.StatusConflict {
					t.Fatalf("SOS aliased fall requestId: HTTP %d %s", rejected.Code, rejected.Body.String())
				}
			} else {
				out := api(t, h.deviceSos, sos)
				if out["incidentState"] != "received" {
					t.Fatalf("initial SOS was not persisted: %#v", out)
				}
				rejected := callAPI(h.deviceReport, fall)
				if rejected.Code != http.StatusConflict {
					t.Fatalf("fall aliased SOS requestId: HTTP %d %s", rejected.Code, rejected.Body.String())
				}
			}
			var incidents, alarms int
			ctx := context.Background()
			if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE device_id=1").Scan(&incidents); err != nil {
				t.Fatal(err)
			}
			if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM alarm_precess WHERE device_id=1").Scan(&alarms); err != nil {
				t.Fatal(err)
			}
			if incidents != 1 || alarms != 1 {
				t.Fatalf("cross-kind collision created effects: incidents=%d alarms=%d", incidents, alarms)
			}
		})
	}
}
