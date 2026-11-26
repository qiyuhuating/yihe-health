package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

func capabilityAdmin(h *Handler, f http.HandlerFunc) http.HandlerFunc {
	return withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 1}, f)
}
func commandSetup(t *testing.T) *Handler {
	t.Helper()
	pool := dbtest.MySQL(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	h := testHandler(t, pool)
	api(t, capabilityAdmin(h, h.commandProfile), map[string]any{"deviceId": 1, "wireType": "K1", "protocol": "yihe-pull-v1", "enabled": true})
	return h
}
func commandCredentials() map[string]any {
	return map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "bindingVersion": 1}
}

func TestCommandQueueReceiptsAndBinding(t *testing.T) {
	h := commandSetup(t)
	create := capabilityAdmin(h, h.commandCreate)
	req := map[string]any{"deviceId": 1, "action": "location", "requestKey": "one", "ttlSeconds": 300}
	id := api(t, create, req)["commandId"]
	if again := api(t, create, req); again["commandId"] != id || again["duplicate"] != true {
		t.Fatal(again)
	}
	req["action"] = "restart"
	if w := callAPI(create, req); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	bad := commandCredentials()
	bad["secret"] = "wrong"
	if w := callAPI(h.commandPull, bad); w.Code != 401 {
		t.Fatal(w.Code)
	}
	cmd := api(t, h.commandPull, commandCredentials())["command"].(map[string]any)
	if cmd["wireAction"] != "get_locationdata" || cmd["channel"] != "gprs" {
		t.Fatal(cmd)
	}
	if empty := api(t, h.commandPull, commandCredentials()); empty["command"] != nil {
		t.Fatal("double delivery", empty)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "hr": 72})
	var state string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT status FROM device_command WHERE id=?", id).Scan(&state); err != nil || state != "sent" {
		t.Fatal("telemetry acknowledged command", state, err)
	}
	ack := commandCredentials()
	ack["commandId"] = id
	ack["deliveryToken"] = "forged"
	ack["result"] = "executed"
	if w := callAPI(h.commandAck, ack); w.Code != 409 {
		t.Fatal(w.Code)
	}
	ack["deliveryToken"] = cmd["deliveryToken"]
	api(t, h.commandAck, ack)
	api(t, h.commandAck, ack)
	ack["result"] = "failed"
	if w := callAPI(h.commandAck, ack); w.Code != 409 {
		t.Fatal("conflicting duplicate", w.Code)
	}
	req["requestKey"] = "two"
	req["action"] = "restart"
	next := api(t, create, req)["commandId"]
	api(t, h.commandPull, commandCredentials())
	mustExec(t, h.DB, "UPDATE device_command SET retry_at=? WHERE id=?", time.Now().Add(-time.Minute), next)
	if err := h.CapabilityTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.DB.QueryRowContext(context.Background(), "SELECT status FROM device_command WHERE id=?", next).Scan(&state); err != nil || state != "timeout" {
		t.Fatal("unsafe retry", state, err)
	}
	req["requestKey"] = "three"
	req["action"] = "location"
	api(t, create, req)
	mustExec(t, h.DB, "UPDATE device SET old_id=NULL WHERE device_id=1")
	if w := callAPI(h.commandAck, ack); w.Code != 409 {
		t.Fatal("old binding ACK", w.Code)
	}
	if err := h.CapabilityTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.DB.QueryRowContext(context.Background(), "SELECT status FROM device_command WHERE request_key='three'").Scan(&state); err != nil || state != "cancelled" {
		t.Fatal(state, err)
	}
}

func TestConcurrentPullAndSafeRedelivery(t *testing.T) {
	h := commandSetup(t)
	api(t, capabilityAdmin(h, h.commandCreate), map[string]any{"deviceId": 1, "action": "location", "requestKey": "concurrent", "ttlSeconds": 300})
	var wg sync.WaitGroup
	out := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); out <- callAPI(h.commandPull, commandCredentials()) }()
	}
	wg.Wait()
	close(out)
	count := 0
	var command map[string]any
	for w := range out {
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		if v["command"] != nil {
			count++
			command = v["command"].(map[string]any)
		}
	}
	if count != 1 {
		t.Fatal("multiple claims", count)
	}
	mustExec(t, h.DB, "UPDATE device_command SET retry_at=?", time.Now().Add(-time.Minute))
	if err := h.CapabilityTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A newly constructed handler proves the queue is not held in process memory.
	second := api(t, testHandler(t, h.DB).commandPull, commandCredentials())["command"].(map[string]any)
	if second["commandId"] != command["commandId"] || second["deliveryToken"] == command["deliveryToken"] {
		t.Fatal(second)
	}
	ack := commandCredentials()
	ack["commandId"] = command["commandId"]
	ack["deliveryToken"] = command["deliveryToken"]
	ack["result"] = "executed"
	if w := callAPI(h.commandAck, ack); w.Code != 409 {
		t.Fatal("stale attempt ACK", w.Code)
	}
	ack["deliveryToken"] = second["deliveryToken"]
	api(t, h.commandAck, ack)
}

func TestDailyRebuildCorrectionsDeletionAndIsolation(t *testing.T) {
	pool := dbtest.MySQL(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	// Deployments may inherit case-insensitive source column collations.
	mustExec(t, pool, "ALTER TABLE health_sample MODIFY source varchar(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL")
	h := testHandler(t, pool)
	admin := capabilityAdmin(h, h.healthDaily)
	req := map[string]any{"oldId": 1, "source": "watch", "sourceDevice": "WATCH001", "metric": "heart_rate", "semantics": "instant", "day": "2026-10-20", "offsetMinutes": 480, "days": 2}
	tm := time.Date(2026, 10, 20, 12, 0, 0, 0, time.FixedZone("", 8*3600))
	s := health.Sample{RecordID: "daily", Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Semantics: "instant", Unit: "count/min", Value: health.Number(80), Start: tm, End: tm, Version: 1}
	otherSource := s
	otherSource.Source = "WATCH"
	otherSource.Value = health.Number(199)
	batch := commandCredentials()
	batch["samples"] = []health.Sample{s, otherSource}
	api(t, h.healthBatch, batch)
	items := api(t, admin, req)["items"].([]any)
	if items[0].(map[string]any)["status"] != "pending" {
		t.Fatal(items)
	}
	if err := h.CapabilityTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	items = api(t, admin, req)["items"].([]any)
	if items[0].(map[string]any)["statistics"].(map[string]any)["mean"] != float64(80) {
		t.Fatal(items)
	}
	// Correcting a sample into the next day must clear the previous day's result.
	s.Version = 2
	s.Start = s.Start.Add(24 * time.Hour)
	s.End = s.Start
	s.Value = health.Number(90)
	batch["samples"] = []health.Sample{s}
	api(t, h.healthBatch, batch)
	items = api(t, admin, req)["items"].([]any)
	if items[0].(map[string]any)["statistics"] != nil {
		t.Fatal("served stale result", items)
	}
	if err := h.CapabilityTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	items = api(t, admin, req)["items"].([]any)
	if items[0].(map[string]any)["statistics"].(map[string]any)["count"] != float64(0) || items[1].(map[string]any)["statistics"].(map[string]any)["mean"] != float64(90) {
		t.Fatal(items)
	}
	s.Version = 3
	s.Deleted = true
	s.Value = nil
	batch["samples"] = []health.Sample{s}
	api(t, h.healthBatch, batch)
	if err := h.CapabilityTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	items = api(t, admin, req)["items"].([]any)
	if items[1].(map[string]any)["statistics"].(map[string]any)["count"] != float64(0) {
		t.Fatal(items)
	}
	mustExec(t, pool, "INSERT INTO family(family_id,family_phone) VALUES(1,'1'),(2,'2')")
	mustExec(t, pool, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	other := withHealthPrincipal(h, session.Principal{Role: auth.RoleFamily, ID: 2}, h.healthDaily)
	if w := callAPI(other, req); w.Code != 403 {
		t.Fatal("daily data leaked", w.Code)
	}
}

func TestFenceDebounceEventsAndCareLink(t *testing.T) {
	pool := dbtest.MySQL(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	h := testHandler(t, pool)
	req := map[string]any{"deviceId": 1, "name": "家", "enabled": true, "coordinateSystem": "WGS84", "timezone": "Asia/Shanghai", "timeBegin": 0, "timeEnd": 86400, "minPoints": 2, "dwellSeconds": 10, "maxAccuracy": 30, "coordinates": [][2]float64{{120, 30}, {120.01, 30}, {120.01, 30.01}, {120, 30.01}, {120, 30}}}
	save := capabilityAdmin(h, h.fenceSave)
	saved := api(t, save, req)
	if saved["deviceApplied"] != false {
		t.Fatal(saved)
	}
	start := time.Now().UTC().Add(-time.Minute)
	point := commandCredentials()
	point["coordinateSystem"] = "WGS84"
	point["accuracy"] = 5
	point["point"] = [2]float64{120.005, 30.005}
	point["occurredAt"] = start
	if got := api(t, h.fencePoint, point)["events"].([]any); len(got) != 0 {
		t.Fatal("first observation emitted entry", got)
	}
	point["point"] = [2]float64{120.02, 30.005}
	point["occurredAt"] = start.Add(time.Second)
	if got := api(t, h.fencePoint, point)["events"].([]any); len(got) != 0 {
		t.Fatal("drift emitted exit", got)
	}
	point["occurredAt"] = start.Add(12 * time.Second)
	events := api(t, h.fencePoint, point)["events"].([]any)
	if len(events) != 1 {
		t.Fatal(events)
	}
	event := events[0].(map[string]any)
	if event["direction"] != "exit" || event["incidentId"] == nil {
		t.Fatal(event)
	}
	if got := api(t, h.fencePoint, point)["events"].([]any); len(got) != 0 {
		t.Fatal("duplicate event", got)
	}
	point["occurredAt"] = start.Add(2 * time.Second)
	point["point"] = [2]float64{120.005, 30.005}
	if got := api(t, h.fencePoint, point)["events"].([]any); len(got) != 0 {
		t.Fatal("old point changed state", got)
	}
	var kind, state string
	if err := pool.QueryRowContext(context.Background(), "SELECT kind,state FROM care_incident WHERE id=?", event["incidentId"]).Scan(&kind, &state); err != nil || kind != "fence_exit" || state != "received" {
		t.Fatal(kind, state, err)
	}
	req["id"] = saved["id"]
	req["revision"] = 0
	if w := callAPI(save, req); w.Code != 409 {
		t.Fatal("stale edit", w.Code)
	}
	req["revision"] = saved["revision"]
	req["enabled"] = false
	api(t, save, req)
	point["occurredAt"] = start.Add(30 * time.Second)
	if got := api(t, h.fencePoint, point)["events"].([]any); len(got) != 0 {
		t.Fatal("disabled fence", got)
	}
	point["coordinateSystem"] = "GCJ02"
	if w := callAPI(h.fencePoint, point); w.Code != 400 {
		t.Fatal("unknown coordinate conversion", w.Code)
	}
}

func TestCareVerdictAndVitalReviewAudit(t *testing.T) {
	pool := dbtest.MySQL(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	seedCareAdminDuty(t, pool, 2, "supervisor")
	h := testHandler(t, pool)
	sos := api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "test-press", "bindingVersion": 1})
	id := sos["incidentId"]
	for _, step := range []struct {
		action string
		actor  int64
	}{{"acknowledge", 1}, {"start", 1}, {"resolve", 1}, {"verify", 2}} {
		f := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: step.actor}, h.careTransition)
		api(t, f, map[string]any{"id": id, "action": step.action, "note": "现场核实后确认误触", "verdict": "false_alarm"})
	}
	items := api(t, capabilityAdmin(h, h.alertReviews), map[string]any{"entity": "care", "recordId": id})["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["verdict"] != "false_alarm" || items[0].(map[string]any)["actorId"] != float64(2) {
		t.Fatal(items)
	}
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "hr": 200})
	var record int64
	if err := pool.QueryRowContext(context.Background(), "SELECT id FROM ratepre WHERE rate_state='1' ORDER BY id DESC LIMIT 1").Scan(&record); err != nil {
		t.Fatal(err)
	}
	req := map[string]any{"recordId": record, "kind": "rate", "verdict": "false_alarm", "reason": "复测正常", "expectedReviewId": 0}
	review := capabilityAdmin(h, h.alertReview)
	out := api(t, review, req)
	if w := callAPI(review, req); w.Code != 409 {
		t.Fatal("lost concurrent review", w.Code)
	}
	req["expectedReviewId"] = out["reviewId"]
	req["verdict"] = "confirmed"
	req["reason"] = "补充证据"
	api(t, review, req)
	items = api(t, capabilityAdmin(h, h.alertReviews), map[string]any{"entity": "ratepre", "recordId": record})["items"].([]any)
	if len(items) != 2 {
		t.Fatal("review history overwritten", items)
	}
}

func TestCapabilityMiddlewareBoundaries(t *testing.T) {
	h := commandSetup(t)
	mux := http.NewServeMux()
	h.Routes(mux)
	wrapped := csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, mux))
	family := h.Sessions.Create(session.Principal{Role: auth.RoleFamily, ID: 1})
	for _, path := range []string{"/api/v1/device-command-profiles", "/api/v1/device-commands", "/api/v1/device-commands/search", "/api/v1/fences", "/api/v1/fences/search", "/api/v1/fence-events/search", "/api/v1/alert-reviews", "/api/v1/alert-reviews/search"} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: family.ID})
		r.Header.Set(csrf.HeaderName, family.CSRFToken)
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/v1/device/commandPull", "/api/v1/device/commandAck", "/api/v1/device/fencePoint"} {
		// Hardware routes must use body authentication rather than cookie authority.
		body := map[string]any{}
		if strings.HasSuffix(path, "commandAck") {
			body = map[string]any{"commandId": 1, "deliveryToken": "x", "result": "executed"}
		}
		if strings.HasSuffix(path, "fencePoint") {
			body = map[string]any{"coordinateSystem": "WGS84", "point": [2]float64{120, 30}, "accuracy": 5, "occurredAt": time.Now().UTC()}
		}
		data, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", path, strings.NewReader(string(data)))
		w := httptest.NewRecorder()
		wrapped.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
