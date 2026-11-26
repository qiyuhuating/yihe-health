package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/familyaddress"
	"yihe-care/internal/session"
)

func TestProtectedFamilyLifecycleMySQL(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	ctx := context.Background()
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	const phone = "13800000002"
	const password = "TestPassword123!"
	mustExec(t, h.DB, "INSERT INTO oldperson(old_id,name,phone) VALUES(1,'one','13800000001'),(2,'two','13800000003')")
	input := map[string]any{"familyName": "family", "familyRelation": "child", "familySex": "女", "familyPhone": phone, "oldPhone": "13800000001", "familyAddress": "synthetic"}
	assertAddress := func(id int64, expected string) string {
		t.Helper()
		var address familyaddress.Record
		if err := h.DB.QueryRowContext(ctx, "SELECT "+familyaddress.Columns+" FROM family WHERE family_id=?", id).
			Scan(&address.Legacy, &address.Cipher, &address.Protected); err != nil {
			t.Fatal(err)
		}
		plain, err := address.Open(h.Box, id)
		if err != nil || address.Legacy != nil || !address.Protected || plain == nil || *plain != expected {
			t.Fatal("family address did not persist as authenticated ciphertext", err)
		}
		return *address.Cipher
	}
	api(t, withHealthPrincipal(h, admin, h.familyAdd), input)
	assertProtected := func(id int64, expected string) {
		t.Helper()
		var plaintext *string
		var cipher, lookup string
		var protected bool
		if err := h.DB.QueryRowContext(ctx, "SELECT family_phone,family_phone_cipher,family_phone_lookup,family_phone_protected FROM family WHERE family_id=?", id).Scan(&plaintext, &cipher, &lookup, &protected); err != nil {
			t.Fatal(err)
		}
		if plaintext != nil || !protected || !strings.HasPrefix(cipher, "sne1:") || strings.Contains(cipher, expected) || len(lookup) != 64 {
			t.Fatal("family primary phone remained plaintext or lost encrypted metadata")
		}
	}
	assertProtected(1, phone)
	assertAddress(1, "synthetic")
	mustExec(t, h.DB, "INSERT INTO invite_code(code,old_id,used) VALUES('FAMILYPROTECT',1,0)")
	api(t, h.inviteJoin, map[string]any{"code": "FAMILYPROTECT", "familyName": "family", "familyPhone": phone, "familyPassword": password, "familyRelation": "child", "familySex": "女", "familyAddress": "激活后地址"})
	assertAddress(1, "激活后地址")
	login := callAPI(h.login, map[string]any{"phone": phone, "password": password})
	if login.Code != 200 || !strings.Contains(login.Body.String(), `"authenticated":true`) {
		t.Fatal("encrypted placeholder could not activate and authenticate")
	}
	owner := session.Principal{Role: auth.RoleFamily, ID: 1, OldID: 1, Phone: phone, DisplayName: "family"}
	for _, query := range []struct {
		handler http.HandlerFunc
		body    map[string]any
		key     string
	}{{h.familySelect, map[string]any{"familyName": ""}, "familyDtos"}, {h.getResidentDetails, map[string]any{"name": ""}, "oldPersonDetails"}} {
		out := api(t, withHealthPrincipal(h, admin, query.handler), query.body)
		rows := out[query.key].([]any)
		if len(rows) < 1 || rows[0].(map[string]any)["familyPhone"] != phone || rows[0].(map[string]any)["familyAddress"] != "激活后地址" {
			t.Fatal("authorized contact API did not preserve its plaintext response contract")
		}
		if query.key == "oldPersonDetails" && (len(rows) != 2 || rows[1].(map[string]any)["familyAddress"] != nil) {
			t.Fatal("unbound resident unexpectedly returned a family address")
		}
	}
	out := api(t, withHealthPrincipal(h, owner, h.familySelectTwo), map[string]any{"familyPhone": phone})
	if item := out["familyDto2"].(map[string]any); item["familyId"] != float64(1) || item["familyAddress"] != "激活后地址" {
		t.Fatal("encrypted phone lookup changed family ownership")
	}
	api(t, withHealthPrincipal(h, owner, h.familyUpdateTwo), map[string]any{"familyId": 1, "familyName": "profile", "familySex": "女", "familyPhone": phone, "familyAddress": "new"})
	assertProtected(1, phone)
	assertAddress(1, "new")
	mustExec(t, h.DB, "INSERT INTO `text`(text_id,title,text_main) VALUES(1,'synthetic','synthetic')")
	api(t, withHealthPrincipal(h, owner, h.createNoticeComment), map[string]any{"textId": 1, "said": "synthetic"})
	api(t, withHealthPrincipal(h, admin, h.createNotice), map[string]any{"title": "synthetic", "textMain": "synthetic", "fileListName": []string{}})
	var recipient string
	if err := h.DB.QueryRowContext(ctx, "SELECT recipient FROM message_outbox WHERE topic='announcement' AND family_id=1 AND channel='sms'").Scan(&recipient); err != nil {
		t.Fatal(err)
	}
	if opened, err := h.Box.Open(recipient, "notification-recipient"); err != nil || opened != phone || recipient == phone {
		t.Fatal("announcement lost or exposed its protected recipient")
	}
	duplicate := map[string]any{"familyName": "duplicate", "familyRelation": "child", "familySex": "女", "familyPhone": phone, "oldPhone": "13800000003", "familyAddress": "synthetic"}
	apiFailure(t, 400, withHealthPrincipal(h, admin, h.familyAdd), duplicate)
	input["familyId"] = 1
	input["familyPhone"] = "13800000004"
	api(t, withHealthPrincipal(h, admin, h.familyUpdate), input)
	assertProtected(1, "13800000004")
	originalAddress := assertAddress(1, "synthetic")
	if old := callAPI(h.login, map[string]any{"phone": phone, "password": password}); old.Code != 401 {
		t.Fatal("reassigned old phone still authenticated")
	}
	newOwner := owner
	newOwner.Phone = "13800000004"
	api(t, withHealthPrincipal(h, newOwner, h.familyUpdatePassword), map[string]any{"familyPhone": newOwner.Phone, "oldPassword": password, "familyPassword": "ChangedPassword123"})
	if got := callAPI(h.login, map[string]any{"phone": newOwner.Phone, "password": "ChangedPassword123"}); got.Code != 200 {
		t.Fatal("password mutation cannot use encrypted phone lookup")
	}
	mustExec(t, h.DB, "INSERT INTO invite_code(code,old_id,used) VALUES('FAMILYDIRECT',2,0)")
	api(t, h.inviteJoin, map[string]any{"code": "FAMILYDIRECT", "familyName": "direct", "familyPhone": "13800000005", "familyPassword": password, "familyRelation": "child", "familySex": "女", "familyAddress": "synthetic"})
	var second int64
	if err := h.DB.QueryRowContext(ctx, "SELECT family_id FROM oldperson WHERE old_id=2").Scan(&second); err != nil {
		t.Fatal(err)
	}
	assertProtected(second, "13800000005")
	foreignAddress := assertAddress(second, "synthetic")
	mustExec(t, h.DB, "UPDATE family SET family_address_cipher=? WHERE family_id=1", foreignAddress)
	apiFailure(t, 500, withHealthPrincipal(h, newOwner, h.familySelectTwo), map[string]any{"familyPhone": newOwner.Phone})
	input["familyName"] = "must not be written"
	apiFailure(t, 500, withHealthPrincipal(h, admin, h.familyUpdate), input)
	var savedName string
	if err := h.DB.QueryRowContext(ctx, "SELECT family_name FROM family WHERE family_id=1").Scan(&savedName); err != nil || savedName != "family" {
		t.Fatal("rejected address edit changed the family profile", err)
	}
	mustExec(t, h.DB, "UPDATE family SET family_address_cipher=? WHERE family_id=1", originalAddress)
	mustExec(t, h.DB, "UPDATE family SET family_phone_cipher='sne1:v2:UNKNOWN:AAAA' WHERE family_id=1")
	if got := callAPI(withHealthPrincipal(h, admin, h.familySelect), map[string]any{"familyName": ""}); got.Code != 500 {
		t.Fatal("corrupt protected phone was treated as empty data")
	}
}
