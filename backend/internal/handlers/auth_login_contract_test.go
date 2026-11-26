package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

// The login response is a status, not a duplicate account profile. Clients
// must fetch the authenticated profile from GET /api/v1/auth/session.
func TestNativeLoginResponseDoesNotExposeAccountDTOs(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	h.Box = box
	password := "TestPassword123!"
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, "INSERT INTO users(user_id,user_name,user_phone,password,role) VALUES(1,'护理主管','13800000001',?,'1')", hash)
	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone,family_password,family_address) VALUES(2,'亲属','13800000002',?,'敏感地址')", hash)
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,family_id) VALUES(3,'长者',2)")

	for _, tc := range []struct {
		name, phone, role string
		oldID             int64
	}{
		{"administrator", "13800000001", auth.RoleAdmin, 0},
		{"family", "13800000002", auth.RoleFamily, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := callAPI(h.login, map[string]any{"phone": tc.phone, "password": password})
			if w.Code != http.StatusOK {
				t.Fatalf("login: HTTP %d: %s", w.Code, w.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 2 || body["authenticated"] != true || body["returnCode"] != "1111" {
				t.Fatalf("account data leaked in login response: %#v", body)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != session.CookieName {
				t.Fatalf("authenticated session cookie missing: %v", cookies)
			}
			current := h.Sessions.Get(cookies[0].Value)
			if current == nil || current.Principal.Role != tc.role || current.Principal.OldID != tc.oldID {
				t.Fatalf("session principal was not persisted: %#v", current)
			}
		})
	}

	w := callAPI(h.login, map[string]any{"phone": "13800000001", "password": "IncorrectPassword123"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong credentials should return HTTP 401, got %d: %s", w.Code, w.Body.String())
	}
	var wrong map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &wrong); err != nil {
		t.Fatal(err)
	}
	if wrong["code"] != "INVALID_CREDENTIALS" || wrong["authenticated"] != nil {
		t.Fatalf("wrong credentials returned unexpected body: %#v", wrong)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("invalid login emitted a session cookie")
	}

	mustExec(t, pool, "INSERT INTO family(family_id,family_name,family_phone,family_password) VALUES(4,'未关联','13800000004',?)", hash)
	unlinked := callAPI(h.login, map[string]any{"phone": "13800000004", "password": password})
	if unlinked.Code != http.StatusForbidden {
		t.Fatalf("unlinked family: HTTP %d: %s", unlinked.Code, unlinked.Body.String())
	}
	if len(unlinked.Result().Cookies()) != 0 {
		t.Fatal("unlinked family emitted a session cookie")
	}
}
