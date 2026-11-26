package handlers

import (
	"context"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func familyPhoneValidationHandler(t *testing.T) *Handler {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	hash, err := auth.Hash("TestPassword123!")
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, h.DB, "INSERT INTO family(family_name,family_phone,family_password,family_address) VALUES('原家属','13800000001',?,'原地址')", hash)
	mustExec(t, h.DB, "INSERT INTO oldperson(old_id,name,family_id,phone) VALUES(1,'已关联',1,'13800000009'),(2,'待关联',NULL,'13800000008')")
	return h
}

func familyPhoneEdit(phone string) map[string]any {
	return map[string]any{"familyId": 1, "familyName": "新姓名", "familyRelation": "子女", "familySex": "女", "familyPhone": phone, "familyAddress": "新地址"}
}

func assertFamilyPhoneUnchanged(t *testing.T, h *Handler) {
	t.Helper()
	var name, phone, address string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT family_name,family_phone,family_address FROM family WHERE family_id=1").Scan(&name, &phone, &address); err != nil {
		t.Fatal(err)
	}
	if name != "原家属" || phone != "13800000001" || address != "原地址" {
		t.Errorf("rejected edit modified account: name=%s phone=%s address=%s", name, phone, address)
	}
}

func TestContinuousFamilyPhoneFormatValidation(t *testing.T) {
	h := familyPhoneValidationHandler(t)
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	update := withHealthPrincipal(h, admin, h.familyUpdate)
	create := withHealthPrincipal(h, admin, h.familyAdd)
	if out := apiFailure(t, 400, update, familyPhoneEdit("abc")); out["returnCode"] == "1111" {
		t.Error("edit accepted a phone that cannot log in")
	}
	assertFamilyPhoneUnchanged(t, h)
	add := familyPhoneEdit("1380000000")
	add["oldPhone"] = "13800000008"
	if out := apiFailure(t, 400, create, add); out["returnCode"] == "1111" {
		t.Error("add accepted an invalid phone")
	}
	var count int
	var bound *int64
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM family").Scan(&count); err != nil || count != 1 {
		t.Errorf("invalid add created an account: count=%d err=%v", count, err)
	}
	if err := h.DB.QueryRowContext(context.Background(), "SELECT family_id FROM oldperson WHERE old_id=2").Scan(&bound); err != nil || bound != nil {
		t.Fatalf("invalid phone changed elder binding: family=%v err=%v", bound, err)
	}
}

func TestContinuousFamilyPhoneEditRejectsOtherAccounts(t *testing.T) {
	h := familyPhoneValidationHandler(t)
	update := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.familyUpdate)
	mustExec(t, h.DB, "INSERT INTO family(family_name,family_phone) VALUES('另一家属','13800000002')")
	mustExec(t, h.DB, "INSERT INTO users(user_id,user_name,user_phone,password,role) SELECT 1,'管理员','13800000003',family_password,'1' FROM family WHERE family_id=1")
	for _, phone := range []string{"13800000002", "13800000003"} {
		w := callAPI(update, familyPhoneEdit(phone))
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"returnCode":"0000"`) {
			t.Errorf("existing account phone %s not rejected as business failure: HTTP %d %s", phone, w.Code, w.Body.String())
		}
		assertFamilyPhoneUnchanged(t, h)
	}
	for _, phone := range []string{"13800000001", "13800000004"} {
		if out := api(t, update, familyPhoneEdit(phone)); out["returnCode"] != "1111" {
			t.Fatalf("valid unchanged/new phone rejected: %v", out)
		}
	}
	loggedIn := callAPI(h.login, map[string]any{"phone": "13800000004", "password": "TestPassword123!"})
	if loggedIn.Code != 200 || len(loggedIn.Result().Cookies()) != 1 {
		t.Fatalf("updated account cannot log in: HTTP %d %s", loggedIn.Code, loggedIn.Body.String())
	}
	current := h.Sessions.Get(loggedIn.Result().Cookies()[0].Value)
	if current == nil || current.Principal.Role != auth.RoleFamily || current.Principal.ID != 1 || current.Principal.OldID != 1 {
		t.Fatalf("phone update changed account ownership: %+v", current)
	}
}
