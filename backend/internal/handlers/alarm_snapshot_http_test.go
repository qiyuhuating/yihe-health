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
	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

func TestAlarmSnapshotHTTPNeverBorrowsHistory(t *testing.T) {
	for _, cached := range []bool{false, true} {
		name := "without_history"
		if cached {
			name = "with_history"
		}
		t.Run(name, func(t *testing.T) {
			h := extraAuditHandler(t)
			seedCareAdminDuty(t, h.DB, 11, "supervisor")
			seedDevice(t, h.DB)
			mux := http.NewServeMux()
			h.Routes(mux)
			server := httptest.NewServer(csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, mux)))
			defer server.Close()
			client := server.Client()
			client.Timeout = 10 * time.Second
			post := func(endpoint string, fields map[string]any, want int) map[string]any {
				t.Helper()
				body, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Post(server.URL+"/api/v1/device/"+endpoint, "application/json", bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				var data map[string]any
				if err := json.NewDecoder(response.Body).Decode(&data); err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != want {
					t.Fatalf("%s: status=%d want=%d body=%v", endpoint, response.StatusCode, want, data)
				}
				return data
			}
			report := func(at string, fields map[string]any, want int) {
				t.Helper()
				fields["deviceNo"], fields["secret"], fields["beTime"] = "WATCH001", testSecret, "2026-10-20 "+at
				post("report", fields, want)
			}
			if cached {
				report("10:00:00", map[string]any{"lng": "121", "lat": "31"}, 200)
			}
			report("11:00:00", map[string]any{"alarm": "fall"}, 200)
			row := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})["positionDtos"].([]any)[0].(map[string]any)
			if row["alarmLng"] != nil || row["alarmLat"] != nil || row["alarmTime"] != "2026-10-20 11:00:00" {
				t.Errorf("alarm without this request's coordinates borrowed history: %v", row)
			}
			if cached && (row["lng"] != "121" || row["lat"] != "31" || row["beTime"] != "2026-10-20 10:00:00") {
				t.Errorf("independent historical location lost: %v", row)
			}
			for _, fields := range []map[string]any{{"alarm": "fall", "lng": "120"}, {"alarm": "fall", "lat": "30"}} {
				report("11:30:00", fields, 400)
			}
			var count int
			if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM alarm_precess").Scan(&count); err != nil || count != 1 {
				t.Fatalf("invalid partial point wrote alarms: count=%d err=%v", count, err)
			}
			// A later ordinary position must not add coordinates to the earlier alarm.
			report("12:00:00", map[string]any{"lng": "122", "lat": "32"}, 200)
			var lng, lat *string
			if err := h.DB.QueryRowContext(context.Background(), "SELECT lng,lat FROM alarm_precess ORDER BY id LIMIT 1").Scan(&lng, &lat); err != nil || lng != nil || lat != nil {
				t.Errorf("unknown alarm snapshot changed: lng=%v lat=%v err=%v", lng, lat, err)
			}
			// A valid explicit zero pair is a real point, not missing data.
			report("13:00:00", map[string]any{"alarm": "fall", "lng": "0", "lat": "0"}, 200)
			if err := h.DB.QueryRowContext(context.Background(), "SELECT lng,lat FROM alarm_precess ORDER BY id DESC LIMIT 1").Scan(&lng, &lat); err != nil || lng == nil || lat == nil || *lng == "0" || *lat == "0" {
				t.Fatalf("fresh point missing or not encrypted: lng=%v lat=%v err=%v", lng, lat, err)
			}
			lng, lat, err := openCoordinates(h.Box, lng, lat)
			if err != nil || lng == nil || lat == nil || *lng != "0" || *lat != "0" {
				t.Fatalf("fresh zero point cannot roundtrip: lng=%v lat=%v err=%v", lng, lat, err)
			}
			// Repeat a SOS with a different point: the first immutable snapshot wins.
			payload := map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1, "requestId": "snapshot-retry", "lng": "0", "lat": "0"}
			first := post("sos", payload, 200)
			payload["lng"], payload["lat"] = "123", "33"
			second := post("sos", payload, 200)
			if first["incidentId"] != second["incidentId"] || second["duplicate"] != true {
				t.Fatalf("retry did not preserve original incident: first=%v second=%v", first, second)
			}
			admin := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelectFamily)
			row = api(t, admin, map[string]any{"oldId": 1})["positionDtos"].([]any)[0].(map[string]any)
			if row["alarmLng"] != "0" || row["alarmLat"] != "0" {
				t.Errorf("retry changed alarm point: %v", row)
			}
		})
	}
}
