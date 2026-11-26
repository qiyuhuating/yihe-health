package handlers

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/config"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func secureHandler(t *testing.T) *Handler {
	t.Helper()
	pool := dbtest.MySQL(t)
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg := config.Config{DataKey: key, SessionBackend: "database"}
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	h := New(pool, store, cfg)
	store.UseDatabase(pool.DB, h.Box)
	password, err := auth.Hash("StrongPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,user_phone,password,role) VALUES(1,'主管一','13800000001',?,'1'),(2,'主管二','13800000002',?,'1')", password, password)
	return h
}
func TestTOTPEnrollmentRecoveryAndCrossReplicaRevocation(t *testing.T) {
	h := secureHandler(t)
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	store.UseDatabase(h.DB.DB, h.Box)
	s := store.Create(session.Principal{Role: auth.RoleAdmin, ID: 1})
	wrap := func(f http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) { f(w, session.WithContext(r, s)) }
	}
	setup := api(t, wrap(h.securityFactor), map[string]any{"action": "setup", "password": "StrongPassword123!"})
	secret := setup["secret"].(string)
	var stored string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT secret FROM auth_factor WHERE actor_id=1").Scan(&stored); err != nil || stored == secret || !strings.HasPrefix(stored, securestore.Prefix) {
		t.Fatal("factor seed plaintext", err)
	}
	code, _ := securestore.TOTP(secret, time.Now().Unix()/30, 6)
	confirmed := api(t, wrap(h.securityFactor), map[string]any{"action": "confirm", "password": "StrongPassword123!", "otp": code})
	codes := confirmed["recoveryCodes"].([]any)
	if len(codes) != 8 || store.Get(s.ID) != nil {
		t.Fatal("enrollment did not revoke peer sessions", confirmed)
	}
	credentials := map[string]any{"phone": "13800000001", "password": "StrongPassword123!"}
	if w := callAPI(h.login, credentials); w.Code != 401 {
		t.Fatal("password-only bypass", w.Code, w.Body.String())
	}
	credentials["otp"] = code
	if w := callAPI(h.login, credentials); w.Code != 401 {
		t.Fatal("used enrollment code replay", w.Code)
	}
	credentials["otp"] = ""
	credentials["recoveryCode"] = codes[0]
	login := callAPI(h.login, credentials)
	if login.Code != http.StatusOK || strings.TrimSpace(login.Body.String()) != `{"authenticated":true,"returnCode":"1111"}` {
		t.Fatal(login.Code, login.Body.String())
	}
	if w := callAPI(h.login, credentials); w.Code != 401 {
		t.Fatal("recovery code reused", w.Code)
	}
	var latest *session.Session
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == session.CookieName {
			latest = store.Get(cookie.Value)
		}
	}
	if latest == nil {
		t.Fatal("verified replica session unavailable")
	}
	next, _ := securestore.TOTP(secret, time.Now().Unix()/30+1, 6)
	disable := func(w http.ResponseWriter, r *http.Request) { h.securityFactor(w, session.WithContext(r, latest)) }
	api(t, disable, map[string]any{"action": "disable", "password": "StrongPassword123!", "otp": next})
	if store.Get(latest.ID) != nil {
		t.Fatal("disabled factor left old sessions alive")
	}
	delete(credentials, "recoveryCode")
	api(t, h.login, credentials)
	var audit int
	h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM login_audit WHERE outcome='success'").Scan(&audit)
	if audit != 2 {
		t.Fatal("successful session audit", audit)
	}
}
func TestCareNotesAreEncryptedAndReadableThroughAuthorizedAPI(t *testing.T) {
	h := secureHandler(t)
	mustExec(t, h.DB, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'supervisor',1),(2,'auditor',1)")
	seedDevice(t, h.DB)
	id := api(t, h.deviceSos, map[string]any{"deviceNo": "WATCH001", "secret": testSecret, "requestId": "test-press", "bindingVersion": 1})["incidentId"]
	note := "现场医疗情况只向授权人员展示"
	for _, action := range []string{"acknowledge", "start", "resolve"} {
		api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 1}, h.careTransition), map[string]any{"id": id, "action": action, "note": note})
	}
	var stored string
	h.DB.QueryRowContext(context.Background(), "SELECT resolution_note FROM care_incident WHERE id=?", id).Scan(&stored)
	if !strings.HasPrefix(stored, securestore.Prefix) || strings.Contains(stored, note) {
		t.Fatal("note stored plaintext")
	}
	out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 2}, h.careEvents), map[string]any{"id": id, "page": 1, "pageSize": 20})
	events := out["events"].([]any)
	if events[len(events)-1].(map[string]any)["note"] != note {
		t.Fatal("authorized note cannot be read", out)
	}
}
func TestAnnouncementWritesOutboxInsideTransaction(t *testing.T) {
	h := secureHandler(t)
	mustExec(t, h.DB, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'supervisor',1)")
	seedDevice(t, h.DB)
	mustExec(t, h.DB, "INSERT INTO family(family_id,family_phone) VALUES(1,'13800000003')")
	mustExec(t, h.DB, "UPDATE oldperson SET family_id=1 WHERE old_id=1")
	api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 1}, h.createNotice), map[string]any{"title": "通知", "textMain": "新的照护公告", "fileListName": []string{}})
	var count int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM message_outbox WHERE topic='announcement'").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
}
