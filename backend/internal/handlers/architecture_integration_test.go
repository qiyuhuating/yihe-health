package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func elderPayload() map[string]any {
	return map[string]any{"name": "长者", "bothdate": "1950-01-01", "sex": "女", "phone": "13800000000", "room": "101", "illness": "", "warn": "", "foodLike": "", "foodHate": "", "drag": "", "customerId": 1, "customerName": "监护人"}
}

func careRequest(t *testing.T, h *Handler, handler http.HandlerFunc, actor int64, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/v1/care/test", bytes.NewReader(data))
	sess := h.Sessions.Create(session.Principal{Role: auth.RoleAdmin, ID: actor})
	req = session.WithContext(req, sess)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func TestArchitectureWatchProfileContract(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO watch_info(device_id,op_time,hrf,hrl,bpf,bpl) VALUES(42,'2026-10-01',110,55,140,90)")
	h := testHandler(t, pool)
	out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.listMonitoringProfiles), map[string]any{"oldName": ""})
	row := out["watchDtos"].([]any)[0].(map[string]any)
	if row["deviceId"] != float64(42) || row["opTime"] != "2026-10-01" {
		t.Fatalf("threshold page cannot identify/edit its row: %+v", row)
	}
}

func TestArchitectureFormattedRecordsAcrossDatabases(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "UPDATE oldperson SET device_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO ratepre(old_id,device_id,hr,rate_state,create_time) VALUES(1,1,72,'1',CURRENT_TIMESTAMP(6))")
	mustExec(t, pool, "INSERT INTO watch_day_info(device_id,step,create_time,old_id,binding_epoch) VALUES(1,100,'2026-10-19',1,1)")
	mustExec(t, pool, "INSERT INTO text(title,text_main,time) VALUES('公告','内容','2026-10-19 08:01:02.123')")
	mustExec(t, pool, "INSERT INTO said(said,said_time) VALUES('评论','2026-10-19 08:01:02.123')")
	h := testHandler(t, pool)
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	for _, handler := range []http.HandlerFunc{
		withHealthPrincipal(h, admin, h.listLatestVitals), withHealthPrincipal(h, admin, h.countVitalAlertDevices),
		withHealthPrincipal(h, admin, h.listLatestActivity), h.listNotices,
	} {
		out := callAPI(handler, map[string]any{})
		if out.Code != 200 {
			t.Fatalf("formatted record query returned %d: %s", out.Code, out.Body.String())
		}
	}
	comments := careRequest(t, h, h.listNoticeComments, 1, map[string]any{"textId": 0})
	if comments.Code != 200 || !strings.Contains(comments.Body.String(), "2026-10-19 08:01:02.123") {
		t.Fatalf("comment timestamp query failed: %d %s", comments.Code, comments.Body.String())
	}
	data := api(t, h.listNotices, map[string]any{})
	if got := data["texts"].([]any)[0].(map[string]any)["time"]; got != "2026-10-19 08:01:02.123" {
		t.Fatalf("millisecond timestamp contract changed: %v", got)
	}
}

func TestArchitectureLifestyleFieldsKeepTheirMeaning(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO oldperson(name,food_like,food_hate,drag) VALUES('长者','米饭','花生','常用药')")
	h := testHandler(t, pool)
	out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.listResidents), map[string]any{"name": ""})
	row := out["oldPeople"].([]any)[0].(map[string]any)
	if row["foodLike"] != "米饭" || row["foodHate"] != "花生" || row["drag"] != "常用药" {
		t.Fatalf("diet and medication fields were interchanged: %+v", row)
	}
}

func TestArchitectureUnboundElderCreation(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'监护人')")
	out := callAPI(withHealthPrincipal(testHandler(t, pool), session.Principal{Role: auth.RoleAdmin, ID: 11}, testHandler(t, pool).createResident), elderPayload())
	if out.Code != 200 || architectureBody(t, out)["returnCode"] != "1111" {
		t.Fatalf("elder creation must precede device binding: %d %s", out.Code, out.Body.String())
	}
	var unbound bool
	if err := pool.QueryRowContext(context.Background(), "SELECT device_id IS NULL FROM oldperson").Scan(&unbound); err != nil || !unbound {
		t.Fatalf("new elder must remain unbound: %v %v", unbound, err)
	}
}

func architectureBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestArchitectureElderEditCannotRewriteBinding(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'监护人')")
	seedDevice(t, pool)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,device_id) VALUES(2,'另一长者',2)")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(2,'OTHER','unused',2)")
	mustExec(t, pool, "UPDATE oldperson SET device_id=1 WHERE old_id=1")
	body := elderPayload()
	body["oldId"], body["deviceId"] = 1, 2
	out := callAPI(withHealthPrincipal(testHandler(t, pool), session.Principal{Role: auth.RoleAdmin, ID: 11}, testHandler(t, pool).updateResident), body)
	if out.Code != http.StatusConflict {
		t.Fatalf("record editing rewrote another device association: %d %s", out.Code, out.Body.String())
	}
	var primary int
	if err := pool.QueryRowContext(context.Background(), "SELECT device_id FROM oldperson WHERE old_id=1").Scan(&primary); err != nil || primary != 1 {
		t.Fatalf("primary device changed: %d %v", primary, err)
	}
}

func TestArchitectureBoundDeviceCannotBeTakenOver(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "UPDATE oldperson SET device_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name) VALUES(2,'另一长者')")
	mustExec(t, pool, "INSERT INTO bind_code(code,old_id,used) VALUES('OTHER',2,0)")
	out := callAPI(testHandler(t, pool).deviceBind, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "code": "OTHER"})
	if out.Code != http.StatusConflict {
		t.Fatalf("binding stole an assigned device: %d %s", out.Code, out.Body.String())
	}
	var owner, used int
	if err := pool.QueryRowContext(context.Background(), "SELECT old_id FROM device WHERE device_id=1").Scan(&owner); err != nil || owner != 1 {
		t.Fatalf("owner changed: %d %v", owner, err)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT used FROM bind_code WHERE code='OTHER'").Scan(&used); err != nil || used != 0 {
		t.Fatalf("rejected binding consumed its code: %d %v", used, err)
	}
}

func TestArchitectureBindingKeepsCustomizedThresholds(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	mustExec(t, pool, "UPDATE device SET old_id=NULL WHERE device_id=1")
	mustExec(t, pool, "INSERT INTO watch_info(device_id,hrf,hrl,bpf,bpl) VALUES(1,125,65,150,95)")
	mustExec(t, pool, "INSERT INTO bind_code(code,old_id,used) VALUES('FIRST',1,0)")
	out := api(t, testHandler(t, pool).deviceBind, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "code": "FIRST"})
	if out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	var high, low int
	if err := pool.QueryRowContext(context.Background(), "SELECT hrf,hrl FROM watch_info WHERE device_id=1").Scan(&high, &low); err != nil || high != 125 || low != 65 {
		t.Fatalf("binding reset customized thresholds: %d %d %v", high, low, err)
	}
}

func TestArchitectureMissingMetricRemainsUnknown(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	api(t, testHandler(t, pool).deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "hr": 72})
	var missing bool
	if err := pool.QueryRowContext(context.Background(), "SELECT bp IS NULL AND pre_state IS NULL FROM ratepre").Scan(&missing); err != nil || !missing {
		t.Fatalf("absent blood pressure became a normal zero: %v %v", missing, err)
	}
	api(t, testHandler(t, pool).deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": 200})
	if err := pool.QueryRowContext(context.Background(), "SELECT sleep IS NULL AND sleep_sate IS NULL FROM watch_day_info").Scan(&missing); err != nil || !missing {
		t.Fatalf("steps-only report invented a normal sleep value: %v %v", missing, err)
	}
}

func TestArchitectureConcurrentBindAndUnbind(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	h := testHandler(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	unbind := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.deviceUnbind)
	for n := 0; n < 20; n++ {
		mustExec(t, pool, "UPDATE device SET old_id=1 WHERE device_id=1")
		mustExec(t, pool, "UPDATE oldperson SET device_id=1 WHERE old_id=1")
		code := fmt.Sprintf("RACE%d", n)
		mustExec(t, pool, "INSERT INTO bind_code(code,old_id,used) VALUES(?,1,0)", code)
		start := make(chan struct{})
		results := make(chan *httptest.ResponseRecorder, 2)
		go func() {
			<-start
			results <- callAPI(h.deviceBind, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "code": code})
		}()
		go func() { <-start; results <- callAPI(unbind, map[string]any{"deviceId": 1}) }()
		close(start)
		for range 2 {
			out := <-results
			if out.Code != 200 {
				t.Fatalf("binding lifecycle deadlock/failure: %d %s", out.Code, out.Body.String())
			}
		}
		var consistent bool
		if err := pool.QueryRowContext(context.Background(), `SELECT (o.device_id IS NULL AND d.old_id IS NULL) OR (o.device_id=d.device_id AND d.old_id=o.old_id) FROM oldperson o,device d WHERE o.old_id=1 AND d.device_id=1`).Scan(&consistent); err != nil || !consistent {
			t.Fatalf("concurrent lifecycle split ownership: %v %v", consistent, err)
		}
	}
}

func TestArchitectureUnbindingKeepsRemainingPrimary(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "UPDATE oldperson SET device_id=1 WHERE old_id=1")
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(2,'SECOND','unused',1)")
	h := testHandler(t, pool)
	api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.deviceUnbind), map[string]any{"deviceId": 1})
	var primary *int64
	if err := pool.QueryRowContext(context.Background(), "SELECT device_id FROM oldperson WHERE old_id=1").Scan(&primary); err != nil || primary == nil || *primary != 2 {
		t.Fatalf("remaining attached device lost its primary reference: %v %v", primary, err)
	}
}

func TestCaregiverQueueAndEventVisibilityAcrossDuties(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 11, "caregiver")
	seedCareAdminDuty(t, pool, 12, "caregiver")
	seedCareAdminDuty(t, pool, 13, "supervisor")
	seedCareAdminDuty(t, pool, 14, "auditor")
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,role) VALUES(15,'未授权管理员','1')")
	mustExec(t, pool, `INSERT INTO care_incident(id,old_id,device_id,kind,state,severity,due_at,acknowledged_by) VALUES
(1,1,1,'sos','received','critical',CURRENT_TIMESTAMP(6),NULL),
(2,1,1,'sos','acknowledged','critical',CURRENT_TIMESTAMP(6),'11'),
(3,1,1,'sos','in_progress','critical',CURRENT_TIMESTAMP(6),'12'),
(4,1,1,'sos','verified','critical',CURRENT_TIMESTAMP(6),'12'),
(5,1,1,'sos','verified','critical',CURRENT_TIMESTAMP(6),'11'),
(6,1,1,'sos','escalated','critical',CURRENT_TIMESTAMP(6),'12')`)
	mustExec(t, pool, "INSERT INTO care_incident_event(incident_id,actor,action) VALUES(1,'0','received'),(2,'11','acknowledge'),(3,'12','start'),(5,'11','verify')")
	h := testHandler(t, pool)
	checkQueue := func(actor int64, state string, want int, forbidden map[float64]bool) {
		t.Helper()
		out := careRequest(t, h, h.careIncidents, actor, map[string]any{"state": state, "page": 1, "pageSize": 20})
		if out.Code != http.StatusOK {
			t.Fatalf("actor %d queue HTTP %d: %s", actor, out.Code, out.Body.String())
		}
		body := architectureBody(t, out)
		items := body["items"].([]any)
		if body["total"] != float64(want) || len(items) != want {
			t.Fatalf("actor %d state %s scope: %v", actor, state, body)
		}
		for _, row := range items {
			if forbidden[row.(map[string]any)["id"].(float64)] {
				t.Fatalf("actor %d can see another caregiver's task: %v", actor, row)
			}
		}
	}
	checkQueue(11, "active", 3, map[float64]bool{3: true})
	checkQueue(12, "active", 3, map[float64]bool{2: true})
	checkQueue(11, "verified", 1, map[float64]bool{4: true})
	checkQueue(12, "verified", 1, map[float64]bool{5: true})
	checkQueue(13, "active", 4, nil)
	checkQueue(14, "active", 4, nil)
	checkQueue(14, "verified", 2, nil)

	for _, id := range []string{"3", "4"} {
		out := careRequest(t, h, h.careEvents, 11, map[string]any{"id": id})
		if out.Code != http.StatusForbidden {
			t.Fatalf("caregiver read another owner's incident %s: %d %s", id, out.Code, out.Body.String())
		}
	}
	for _, id := range []string{"1", "2", "5"} {
		out := careRequest(t, h, h.careEvents, 11, map[string]any{"id": id})
		if out.Code != http.StatusOK {
			t.Fatalf("caregiver denied visible incident %s: %d %s", id, out.Code, out.Body.String())
		}
	}
	if out := careRequest(t, h, h.careEvents, 14, map[string]any{"id": "3"}); out.Code != http.StatusOK {
		t.Fatalf("auditor denied read-only history: %d %s", out.Code, out.Body.String())
	}
	for _, handler := range []http.HandlerFunc{h.careIncidents, h.careEvents} {
		if out := careRequest(t, h, handler, 15, map[string]any{"id": "3"}); out.Code != http.StatusForbidden {
			t.Fatalf("unassigned admin accessed care data: %d %s", out.Code, out.Body.String())
		}
	}
	if out := careRequest(t, h, h.careTransition, 14, map[string]any{"id": "1", "action": "acknowledge"}); out.Code != http.StatusForbidden {
		t.Fatalf("auditor modified task: %d %s", out.Code, out.Body.String())
	}
}

func TestArchitectureCareQueuePaginationAndDuty(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 2, "auditor")
	for n := 1; n <= 60; n++ {
		state := "received"
		if n > 55 {
			state = "verified"
		}
		mustExec(t, pool, "INSERT INTO care_incident(old_id,device_id,kind,state,severity,due_at) VALUES(1,1,'sos',?,'critical',CURRENT_TIMESTAMP(6))", state)
	}
	h := testHandler(t, pool)
	out := careRequest(t, h, h.careIncidents, 2, map[string]any{"page": 2, "pageSize": 20, "state": "active"})
	data := architectureBody(t, out)
	if out.Code != 200 || data["total"] != float64(55) || data["duty"] != "auditor" || len(data["items"].([]any)) != 20 {
		t.Fatalf("queue has no complete paging/permission contract: %d %+v", out.Code, data)
	}
	out = careRequest(t, h, h.careIncidents, 2, map[string]any{"page": 1, "pageSize": 20, "state": "verified"})
	data = architectureBody(t, out)
	if data["total"] != float64(5) || len(data["items"].([]any)) != 5 {
		t.Fatalf("completed cases are not independently accessible: %+v", data)
	}
}

func TestArchitectureVerificationClearsOnlyLinkedAlarm(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 2, "supervisor")
	h := testHandler(t, pool)
	firstIncident := api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "verify-first", "bindingVersion": 1})["incidentId"]
	secondIncident := api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "verify-second", "bindingVersion": 1})["incidentId"]
	mustExec(t, pool, "UPDATE care_incident SET state='resolved',resolved_by='1' WHERE id=?", firstIncident)
	out := careRequest(t, h, h.careTransition, 2, map[string]any{"id": fmt.Sprint(firstIncident), "action": "verify", "note": "已独立复核"})
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	var first, second string
	if err := pool.QueryRowContext(context.Background(), `SELECT a.status,b.status FROM care_incident i
		JOIN alarm_precess a ON a.id=i.source_alarm_id
		JOIN care_incident j ON j.id=? JOIN alarm_precess b ON b.id=j.source_alarm_id
		WHERE i.id=?`, secondIncident, firstIncident).Scan(&first, &second); err != nil || first != "0" || second != "1" {
		t.Fatalf("verification did not clear precisely its own alarm: %s %s %v", first, second, err)
	}
	var active bool
	if err := pool.QueryRowContext(context.Background(), "SELECT active FROM resident_location_alert_state WHERE old_id=1").Scan(&active); err != nil || !active {
		t.Fatalf("verification cleared the later incident's snapshot: active=%v err=%v", active, err)
	}
}

func TestArchitectureCareHistoryHasNoSilentLimit(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "auditor")
	mustExec(t, pool, "INSERT INTO care_incident(id,old_id,device_id,kind,severity,due_at) VALUES(1,1,1,'sos','critical',CURRENT_TIMESTAMP(6))")
	for n := 1; n <= 105; n++ {
		mustExec(t, pool, "INSERT INTO care_incident_event(incident_id,actor,action,note) VALUES(1,'1','note',?)", fmt.Sprint(n))
	}
	h := testHandler(t, pool)
	out := careRequest(t, h, h.careEvents, 1, map[string]any{"id": "1", "page": 3, "pageSize": 50})
	if out.Code != 200 {
		t.Fatalf("history lookup rejected: HTTP %d %s", out.Code, out.Body.String())
	}
	data := architectureBody(t, out)
	if data["total"] != float64(105) || len(data["events"].([]any)) != 5 {
		t.Fatalf("audit history silently truncates later events: HTTP %d total=%v rows=%d", out.Code, data["total"], len(data["events"].([]any)))
	}
}

func TestArchitectureCareNoteAcceptsChineseCharacters(t *testing.T) {
	pool := testDatabase(t)
	seedDevice(t, pool)
	seedCareAdminDuty(t, pool, 1, "caregiver")
	seedCareAdminDuty(t, pool, 2, "caregiver")
	mustExec(t, pool, "INSERT INTO care_incident(id,old_id,device_id,kind,state,severity,due_at) VALUES(1,1,1,'sos','in_progress','critical',CURRENT_TIMESTAMP(6))")
	mustExec(t, pool, "UPDATE care_incident SET acknowledged_by='1' WHERE id=1")
	h := testHandler(t, pool)
	denied := careRequest(t, h, h.careTransition, 2, map[string]any{"id": "1", "action": "resolve", "note": "代他人结案"})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unassigned caregiver changed another caregiver's task: %d %s", denied.Code, denied.Body.String())
	}
	var before string
	if err := pool.QueryRowContext(context.Background(), "SELECT state FROM care_incident WHERE id=1").Scan(&before); err != nil || before != "in_progress" {
		t.Fatalf("denied resolve changed incident: %q %v", before, err)
	}
	out := careRequest(t, h, h.careTransition, 1, map[string]any{"id": "1", "action": "resolve", "note": strings.Repeat("护理记录", 300)})
	if out.Code != 200 {
		t.Fatalf("1200 Chinese characters must fit the 2000-character limit: %d", out.Code)
	}
}

func TestArchitectureDiagnosticsExposeMissingGateway(t *testing.T) {
	pool := testDatabase(t)
	seedCareAdminDuty(t, pool, 1, "supervisor")
	mustExec(t, pool, "INSERT INTO care_worker_heartbeat(singleton,last_seen_at,started_at) VALUES(true,CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))")
	t.Setenv("CARE_NOTIFY_WEBHOOK_URL", "")
	t.Setenv("CARE_NOTIFY_WEBHOOK_TOKEN", "")
	t.Setenv("CARE_RECEIPT_SECRET", "")
	h := testHandler(t, pool)
	out := careRequest(t, h, h.careDiagnostics, 1, map[string]any{})
	data := architectureBody(t, out)
	if data["workerHealthy"] != true || data["notificationConfigured"] != false || data["receiptConfigured"] != false {
		t.Fatalf("heartbeat masks missing delivery configuration: %+v", data)
	}
}
