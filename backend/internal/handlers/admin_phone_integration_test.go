package handlers

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestProtectedAdministratorSetupAndLogin(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	h.Cfg.DesktopToken = "synthetic-admin-setup"
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	if err != nil {
		t.Fatal(err)
	}
	h.Box = box
	request := httptest.NewRequest("POST", "/api/v1/setup", strings.NewReader(`{"name":"synthetic","phone":"13800000001","password":"TestPassword123"}`))
	request.Header.Set("X-Yihe-Desktop", h.Cfg.DesktopToken)
	response := httptest.NewRecorder()
	h.setup(response, request)
	if response.Code != 200 {
		t.Fatalf("encrypted setup: %d %s", response.Code, response.Body.String())
	}
	var id int64
	var legacy, cipher, index *string
	var protected bool
	if err := pool.QueryRowContext(context.Background(), `SELECT user_id,user_phone,user_phone_cipher,user_phone_lookup,user_phone_protected FROM users`).Scan(&id, &legacy, &cipher, &index, &protected); err != nil {
		t.Fatal(err)
	}
	if legacy != nil || cipher == nil || index == nil || !protected || strings.Contains(*cipher, "13800000001") {
		t.Fatal("administrator phone not protected at rest")
	}
	login := callAPI(h.login, map[string]any{"phone": "13800000001", "password": "TestPassword123"})
	if login.Code != 200 || !strings.Contains(login.Body.String(), `"authenticated":true`) {
		t.Fatalf("encrypted administrator login: %d %s", login.Code, login.Body.String())
	}
	lookup := callAPI(withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: id}, h.userSelect), map[string]any{"userPhone": "13800000001"})
	if lookup.Code != 200 || !strings.Contains(lookup.Body.String(), `"userPhone":"13800000001"`) {
		t.Fatalf("protected account lookup: %d %s", lookup.Code, lookup.Body.String())
	}
	duplicate := callAPI(withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: id}, h.familyAdd), map[string]any{
		"familyName": "synthetic", "familyRelation": "child", "familySex": "男",
		"familyPhone": "13800000001", "oldPhone": "13800000002", "familyAddress": "synthetic",
	})
	if duplicate.Code != 400 {
		t.Fatalf("encrypted phone collision was accepted: %d %s", duplicate.Code, duplicate.Body.String())
	}
}
