package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"yihe-care/internal/auth"
	"yihe-care/internal/csrf"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
	"testing"
	"time"
)

func extraAuditHandler(t *testing.T) *Handler {
	p := testDatabase(t)
	h := testHandler(t, p)
	h.Box, _ = securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("A", 32))))
	h.Sessions.UseDatabase(p.DB, h.Box)
	return h
}

func TestExtraAuditFamilyOnboarding(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	p := h.DB
	mustExec(t, p, "INSERT INTO oldperson(old_id,name,phone) VALUES(1,'老人','13800000001')")
	added := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.familyAdd), map[string]any{"familyName": "家属", "familyRelation": "子女", "familySex": "女", "familyPhone": "13800000002", "oldPhone": "13800000001", "familyAddress": "测试"})
	if added["returnCode"] != "1111" {
		t.Fatal(added)
	}
	mustExec(t, p, "INSERT INTO invite_code(code,old_id,used) VALUES('AUDITJOIN',1,0)")
	join := api(t, h.inviteJoin, map[string]any{"code": "AUDITJOIN", "familyName": "家属", "familyPhone": "13800000002", "familyPassword": "TestPassword123!", "familyRelation": "子女", "familySex": "女", "familyAddress": "测试"})
	login := callAPI(h.login, map[string]any{"phone": "13800000002", "password": "TestPassword123!"})
	var missing bool
	if err := p.QueryRowContext(context.Background(), "SELECT family_password IS NULL FROM family WHERE family_id=1").Scan(&missing); err != nil {
		t.Fatal(err)
	}
	t.Logf("admin add=%v; passwordNULL=%v; login HTTP=%d; join=%v", added, missing, login.Code, join)
	if missing || login.Code != 200 || join["returnCode"] != "1111" {
		t.Fatal("pre-created family could not activate and log in")
	}
	mustExec(t, p, "INSERT INTO invite_code(code,old_id,used) VALUES('AUDITRESET',1,0)")
	retry := apiFailure(t, 400, h.inviteJoin, map[string]any{"code": "AUDITRESET", "familyName": "其他人", "familyPhone": "13800000002", "familyPassword": "OtherPassword123!", "familyRelation": "子女", "familySex": "女", "familyAddress": "测试"})
	if retry["returnCode"] == "1111" {
		t.Fatal("invitation reset an already active account")
	}
}

func TestExtraAuditPhoneReuse(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	p := h.DB
	mustExec(t, p, "INSERT INTO family(family_id,family_name,family_phone,family_address) VALUES(1,'甲','13800000001','甲地址')")
	oldSession := h.Sessions.Create(session.Principal{Role: auth.RoleFamily, ID: 1, Phone: "13800000001"})
	updated := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.familyUpdate), map[string]any{"familyId": 1, "familyName": "甲", "familyPhone": "13800000003", "familyRelation": "子女", "familySex": "女", "familyAddress": "甲地址"})
	if updated["returnCode"] != "1111" {
		t.Fatal(updated)
	}
	mustExec(t, p, "INSERT INTO family(family_id,family_name,family_phone,family_address) VALUES(2,'乙','13800000001','乙隐私地址')")
	request := httptest.NewRequest("POST", "/api/v1/family/selectTwo", strings.NewReader(`{"familyPhone":"13800000001"}`))
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: oldSession.ID})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-TOKEN", oldSession.CSRFToken)
	recorder := httptest.NewRecorder()
	csrf.Middleware(h.Sessions, auth.Middleware(h.Sessions, http.HandlerFunc(h.familySelectTwo))).ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatalf("HTTP %d: %s", recorder.Code, recorder.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	t.Logf("old actor ID=%d; selected=%v", oldSession.Principal.ID, out)
	if out["familyDto2"] != nil {
		t.Fatal("stale phone session should not select a different account")
	}
}

func TestExtraAuditFamilyOverwrite(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	p := h.DB
	mustExec(t, p, "INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'原家属','13800000002')")
	mustExec(t, p, "INSERT INTO oldperson(old_id,name,phone,family_id) VALUES(1,'老人','13800000001',1)")
	out := callAPI(withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.familyAdd), map[string]any{"familyName": "新家属", "familyRelation": "子女", "familySex": "女", "familyPhone": "13800000003", "oldPhone": "13800000001", "familyAddress": "测试"})
	var bound int64
	if err := p.QueryRowContext(context.Background(), "SELECT family_id FROM oldperson WHERE old_id=1").Scan(&bound); err != nil {
		t.Fatal(err)
	}
	t.Logf("add HTTP=%d; original familyId=1; now familyId=%d", out.Code, bound)
	var count int
	if err := p.QueryRowContext(context.Background(), "SELECT count(*) FROM family").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if out.Code != 409 || bound != 1 || count != 1 {
		t.Fatal("add must reject existing binding and roll back the new family")
	}
}

func TestExtraAuditOutOfOrderTelemetry(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	p := h.DB
	seedDevice(t, p)
	for _, v := range []struct {
		at    string
		steps int
		lng   string
	}{{"2026-10-20 10:00:00", 5000, "120"}, {"2026-10-20 09:00:00", 1000, "119"}} {
		out := api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": v.steps, "lng": v.lng, "lat": "30", "beTime": v.at})
		if out["returnCode"] != "1111" {
			t.Fatal(out)
		}
	}
	var steps int
	if err := p.QueryRowContext(context.Background(), "SELECT step FROM watch_day_info WHERE device_id=1").Scan(&steps); err != nil {
		t.Fatal(err)
	}
	pos := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})
	t.Logf("steps after 10:00 then 09:00=%d; position=%v", steps, pos)
	if steps != 5000 {
		t.Fatal("late older snapshot replaced the newer daily steps")
	}
	item := pos["positionDtos"].([]any)[0].(map[string]any)
	if item["lng"] != "120" || item["beTime"] != "2026-10-20 10:00:00" {
		t.Fatal("late older report replaced current location")
	}
	var sealed string
	if err := p.QueryRowContext(context.Background(), "SELECT last_lng FROM device WHERE device_id=1").Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	lng, err := h.Box.Open(sealed, coordinateScope)
	if err != nil || lng != "120" {
		t.Fatalf("device last coordinate regressed: %s %v", lng, err)
	}
	// A newer correction may lower the count; missing steps must not advance its clock.
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": 4000, "beTime": "2026-10-20 11:00:00"})
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "sleep": 8, "beTime": "2026-10-20 12:00:00"})
	api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "step": 4500, "beTime": "2026-10-20 11:30:00"})
	if err := p.QueryRowContext(context.Background(), "SELECT step FROM watch_day_info WHERE device_id=1").Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if steps != 4500 {
		t.Fatalf("valid correction or partial report ordering failed: %d", steps)
	}
}

func TestExtraAuditPositionTimestamp(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	p := h.DB
	seedDevice(t, p)
	out := api(t, h.deviceReport, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "lng": "120", "lat": "30", "beTime": "2026-10-20 08:00:00"})
	if out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	mustExec(t, p, "INSERT INTO alarm_precess(old_id,device_id,status,be_time) VALUES(1,1,'1',?)", time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC))
	pos := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.positionSelect), map[string]any{"name": ""})
	item := pos["positionDtos"].([]any)[0].(map[string]any)
	t.Logf("location sampled at 08:00; newer alarm at 10:00; displayed=%v", item)
	if item["beTime"] != "2026-10-20 08:00:00" {
		t.Fatal("location timestamp was replaced by unrelated alarm time")
	}
}
