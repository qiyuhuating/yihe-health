package handlers

import (
	"context"
	"net/http/httptest"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestContinuousElderGuardianMustExist(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	create := withHealthPrincipal(h, admin, h.createResident)
	update := withHealthPrincipal(h, admin, h.updateResident)
	mustExec(t, h.DB, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'甲'),(2,'乙')")
	body := elderPayload()
	body["customerId"] = 999
	if out := apiFailure(t, 400, create, body); out["returnCode"] == "1111" {
		t.Error("elder creation accepted a nonexistent guardian")
	}
	var count int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM oldperson").Scan(&count); err != nil || count != 0 {
		t.Fatalf("invalid guardian wrote an elder: count=%d err=%v", count, err)
	}
	body["customerId"] = 1
	if out := api(t, create, body); out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	var oldID int64
	if err := h.DB.QueryRowContext(context.Background(), "SELECT old_id FROM oldperson").Scan(&oldID); err != nil {
		t.Fatal(err)
	}
	body["oldId"], body["customerId"], body["name"] = oldID, 999, "不应写入"
	if out := apiFailure(t, 400, update, body); out["returnCode"] == "1111" {
		t.Error("elder edit accepted a nonexistent guardian")
	}
	var guardian int64
	var name string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT customer_id,name FROM oldperson WHERE old_id=?", oldID).Scan(&guardian, &name); err != nil || guardian != 1 || name != "长者" {
		t.Fatalf("rejected edit changed the profile: guardian=%d name=%s err=%v", guardian, name, err)
	}
	body["customerId"], body["name"] = 2, "长者"
	if out := api(t, update, body); out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	detail := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.getResidentDetails), map[string]any{"name": ""})["oldPersonDetails"].([]any)[0].(map[string]any)
	if detail["customerId"] != float64(2) || detail["customerName"] != "乙" {
		t.Fatalf("valid guardian reassignment lost: %v", detail)
	}
}

func TestContinuousGuardianDeleteKeepsAssignedProfiles(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	authorizedDelete := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.deleteGuardian)
	mustExec(t, h.DB, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'甲'),(2,'乙')")
	mustExec(t, h.DB, "INSERT INTO oldperson(old_id,name,customer_id) VALUES(1,'长者',1)")
	if out := apiFailure(t, 400, authorizedDelete, map[string]any{"customerId": 1}); out["returnCode"] == "1111" {
		t.Error("deleted a guardian still assigned to an elder")
	}
	var count int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM oldperson o JOIN customer c ON c.customer_id=o.customer_id WHERE o.old_id=1").Scan(&count); err != nil || count != 1 {
		t.Fatalf("guardian reference became dangling: count=%d err=%v", count, err)
	}
	if out := api(t, authorizedDelete, map[string]any{"customerId": 2}); out["returnCode"] != "1111" {
		t.Fatalf("unassigned guardian could not be deleted: %v", out)
	}
	if out := api(t, withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.deleteResident), map[string]any{"oldId": 1}); out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	if out := api(t, authorizedDelete, map[string]any{"customerId": 1}); out["returnCode"] != "1111" {
		t.Fatalf("guardian could not be deleted after elder removal: %v", out)
	}
}

func TestContinuousGuardianAgeBoundsDoNotWriteInvalidProfiles(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	principal := session.Principal{Role: auth.RoleAdmin, ID: 11}
	authorizedCreate := withHealthPrincipal(h, principal, h.createGuardian)
	authorizedUpdate := withHealthPrincipal(h, principal, h.updateGuardian)
	mustExec(t, h.DB, "INSERT INTO customer(customer_id,customer_name,customer_age) VALUES(1,'原姓名',40)")
	body := map[string]any{"customerId": 1, "customerName": "不应写入", "customerSex": "女", "customerPhone": "13800000001", "customerAddress": "测试"}
	for _, age := range []any{-1, 151, 1.5, "invalid"} {
		body["customerAge"] = age
		for _, handler := range []struct {
			name string
			call func() int
		}{{"add", func() int { return callAPI(authorizedCreate, body).Code }}, {"edit", func() int { return callAPI(authorizedUpdate, body).Code }}} {
			if code := handler.call(); code != 400 {
				t.Errorf("%s age=%v: HTTP %d, want 400", handler.name, age, code)
			}
		}
	}
	var count, age int
	var name string
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM customer").Scan(&count); err != nil || count != 1 {
		t.Fatalf("invalid ages created guardians: count=%d err=%v", count, err)
	}
	if err := h.DB.QueryRowContext(context.Background(), "SELECT customer_age,customer_name FROM customer WHERE customer_id=1").Scan(&age, &name); err != nil || age != 40 || name != "原姓名" {
		t.Fatalf("invalid ages overwrote a guardian: age=%d name=%s err=%v", age, name, err)
	}
	for _, valid := range []any{0, "150"} {
		body["customerAge"] = valid
		if out := api(t, authorizedUpdate, body); out["returnCode"] != "1111" {
			t.Fatalf("valid age %v rejected: %v", valid, out)
		}
	}
}

func TestContinuousGuardianCreateAndDeleteKeepReferencesValid(t *testing.T) {
	h := extraAuditHandler(t)
	seedCareAdminDuty(t, h.DB, 11, "supervisor")
	create := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.createResident)
	authorizedDelete := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.deleteGuardian)
	mustExec(t, h.DB, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'监护人')")
	start := make(chan struct{})
	created := make(chan *httptest.ResponseRecorder, 1)
	deleted := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		<-start
		created <- callAPI(create, elderPayload())
	}()
	go func() {
		<-start
		deleted <- callAPI(authorizedDelete, map[string]any{"customerId": 1})
	}()
	close(start)
	createResponse, deleteResponse := <-created, <-deleted
	if (createResponse.Code != 200 && createResponse.Code != 400) || (deleteResponse.Code != 200 && deleteResponse.Code != 400) {
		t.Fatalf("concurrent requests must finish without server errors: create=%d %s delete=%d %s", createResponse.Code, createResponse.Body.String(), deleteResponse.Code, deleteResponse.Body.String())
	}
	createResult, deleteResult := architectureBody(t, createResponse), architectureBody(t, deleteResponse)
	createOK, deleteOK := createResult["returnCode"] == "1111", deleteResult["returnCode"] == "1111"
	if createOK == deleteOK {
		t.Fatalf("assignment and parent deletion must have exactly one winner: create=%v delete=%v", createResult, deleteResult)
	}
	var dangling int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT count(*) FROM oldperson o LEFT JOIN customer c ON c.customer_id=o.customer_id WHERE o.customer_id IS NOT NULL AND c.customer_id IS NULL").Scan(&dangling); err != nil || dangling != 0 {
		t.Fatalf("concurrent requests left dangling references: count=%d err=%v", dangling, err)
	}
}
