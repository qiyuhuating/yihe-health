package handlers

import (
	"context"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestDeviceIntegerInputBounds(t *testing.T) {
	for _, input := range []any{"9223372036854775808", float64(1 << 63), 1e100, math.NaN(), math.Inf(1), 1.5, -1.0, "100001"} {
		w := httptest.NewRecorder()
		if n, ok := intOr400(w, map[string]any{"page": input}, "page", 1, 1, 100000); ok || n != 0 || w.Code != 400 {
			t.Errorf("invalid page %v accepted: value=%d ok=%v HTTP=%d", input, n, ok, w.Code)
		}
	}
	for _, input := range []any{nil, "", " 1 ", 1.0, "100000", 100000.0} {
		w := httptest.NewRecorder()
		if n, ok := intOr400(w, map[string]any{"page": input}, "page", 1, 1, 100000); !ok || n < 1 || n > 100000 {
			t.Errorf("valid page %v rejected: value=%d ok=%v", input, n, ok)
		}
	}
}

func TestDeviceRegistrationListContract(t *testing.T) {
	h := extraAuditHandler(t)
	pool := h.DB
	seedCareAdminDuty(t, pool, 11, "supervisor")
	principal := session.Principal{Role: auth.RoleAdmin, ID: 11}
	authorizedCreate := withHealthPrincipal(h, principal, h.deviceCreate)
	authorizedList := withHealthPrincipal(h, principal, h.deviceList)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,phone) VALUES(1,'登记长者','13800000001')")
	created := api(t, authorizedCreate, map[string]any{"oldId": 1})
	id := int64(created["deviceId"].(float64))
	deviceNo, secret := created["deviceNo"].(string), created["secret"].(string)
	var alias, model, storedSecret string
	var owner, primaryDevice, profiles int64
	if err := pool.QueryRowContext(context.Background(), "SELECT alias,model,old_id,secret FROM device WHERE device_id=?", id).Scan(&alias, &model, &owner, &storedSecret); err != nil {
		t.Fatal(err)
	}
	if alias != "新手表" || model != "HarmonyOS-Wear" || owner != 1 || storedSecret == "" || storedSecret == secret {
		t.Fatalf("registration defaults, owner or secret storage changed: alias=%s model=%s owner=%d", alias, model, owner)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT device_id FROM oldperson WHERE old_id=1").Scan(&primaryDevice); err != nil || primaryDevice != id {
		t.Fatalf("primary device not linked: %d %v", primaryDevice, err)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM watch_info WHERE device_id=? AND hrf=110 AND hrl=55 AND bpf=140 AND bpl=90", id).Scan(&profiles); err != nil || profiles != 1 {
		t.Fatalf("default thresholds not initialized: %d %v", profiles, err)
	}
	mustExec(t, pool, "UPDATE device SET last_lng=?,last_lat=?,last_time=?,battery=67 WHERE device_id=?", sealCoordinate(h.Box, "121.5"), sealCoordinate(h.Box, "31.2"), time.Now().UTC().Add(-30*time.Second), id)
	listed := api(t, authorizedList, map[string]any{"query": strings.ToLower(deviceNo), "state": "online"})
	items, ok := listed["devices"].([]any)
	if !ok || len(items) != 1 || listed["total"] != float64(1) || listed["onlineCount"] != float64(1) {
		t.Fatalf("registered device missing from filtered list: %v", listed)
	}
	item := items[0].(map[string]any)
	if item["deviceId"] != float64(id) || item["oldPersonName"] != "登记长者" || item["lastLng"] != "121.5" || item["lastLat"] != "31.2" || item["online"] != float64(1) || item["bindTime"] == nil {
		t.Fatalf("device list projection changed: %v", item)
	}
	if _, exposed := item["secret"]; exposed {
		t.Fatal("device list exposed registration secret")
	}
	for _, invalid := range []map[string]any{{"oldId": "invalid"}, {"alias": strings.Repeat("字", 33)}} {
		if result := callAPI(authorizedCreate, invalid); result.Code != 400 {
			t.Fatalf("invalid registration accepted: HTTP %d", result.Code)
		}
	}
	var devices int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM device").Scan(&devices); err != nil || devices != 1 {
		t.Fatalf("invalid input created a device: %d %v", devices, err)
	}
}

func TestGenericIntegerParamRejectsOverflowAndNonFiniteValues(t *testing.T) {
	for _, input := range []any{1e100, -1e100, float64(1 << 63), math.NaN(), math.Inf(1), math.Inf(-1), 1.1} {
		if value, ok := integerParam(input); ok {
			t.Fatalf("invalid integer input %v accepted as %d", input, value)
		}
	}
	for _, input := range []any{int64(1), float64(0), float64(9007199254740991), "-9223372036854775808", "9223372036854775807"} {
		if _, ok := integerParam(input); !ok {
			t.Fatalf("valid integer input %v rejected", input)
		}
	}
}
