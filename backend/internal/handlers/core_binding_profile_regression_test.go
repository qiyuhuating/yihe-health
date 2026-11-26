package handlers

import (
	"context"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

func TestContinuousAmbiguousElderPhone(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO oldperson(old_id,name,phone) VALUES(1,'甲','13800000001'),(2,'乙','13800000001')")
	w := callAPI(withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.familyAdd), map[string]any{"familyName": "家属", "familyRelation": "子女", "familySex": "女", "familyPhone": "13800000003", "oldPhone": "13800000001", "familyAddress": "测试"})
	var families, bound int
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM family").Scan(&families); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(context.Background(), "SELECT count(*) FROM oldperson WHERE family_id IS NOT NULL").Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || families != 0 || bound != 0 {
		t.Fatalf("ambiguous phone selected an arbitrary elder: status=%d families=%d bound=%d body=%s", w.Code, families, bound, w.Body.String())
	}
}

func TestContinuousElderBirthdateAndAge(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	seedCareAdminDuty(t, pool, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	create := withHealthPrincipal(h, admin, h.createResident)
	update := withHealthPrincipal(h, admin, h.updateResident)
	list := withHealthPrincipal(h, admin, h.listResidents)
	details := withHealthPrincipal(h, admin, h.getResidentDetails)
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'监护人')")
	now := time.Now()
	body := elderPayload()
	body["bothdate"] = now.AddDate(-80, 0, 1).Format("2006-01-02")
	if out := api(t, create, body); out["returnCode"] != "1111" {
		t.Fatal(out)
	}
	item := api(t, list, map[string]any{"name": ""})["oldPeople"].([]any)[0].(map[string]any)
	if item["age"] != float64(79) {
		t.Errorf("age before birthday=%v want=79", item["age"])
	}
	// A historical cache must not prevent the displayed age advancing.
	mustExec(t, pool, "UPDATE oldperson SET age=1 WHERE old_id=1")
	item = api(t, details, map[string]any{"name": ""})["oldPersonDetails"].([]any)[0].(map[string]any)
	if item["age"] != float64(79) {
		t.Errorf("detail returned stale stored age=%v", item["age"])
	}
	for _, birth := range []string{"2026-02-31", "not-a-date", now.AddDate(0, 0, 1).Format("2006-01-02")} {
		body["bothdate"] = birth
		if w := callAPI(create, body); w.Code != http.StatusBadRequest {
			t.Errorf("invalid birthdate=%q add HTTP=%d", birth, w.Code)
		}
		body["oldId"] = 1
		if w := callAPI(update, body); w.Code != http.StatusBadRequest {
			t.Errorf("invalid birthdate=%q update HTTP=%d", birth, w.Code)
		}
	}
}
