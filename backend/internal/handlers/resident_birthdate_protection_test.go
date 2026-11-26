package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"yihe-care/internal/residentprofile"
)

func TestResidentBirthdateBackfillMySQL(t *testing.T) {
	h, admin, body := residentProfileFixture(t)
	ctx := context.Background()
	mustExec(t, h.DB, `INSERT INTO oldperson(old_id,name,bothdate,age) VALUES
 (2,'legacy with date','1956-02-29',17),(3,'second legacy date','1949-12-31',40),
 (4,'legacy without date',NULL,73),(5,'fixture for wrong row',NULL,NULL)`)
	var original string
	if err := h.DB.QueryRowContext(ctx, "SELECT birthdate_cipher FROM oldperson WHERE old_id=1").Scan(&original); err != nil {
		t.Fatal(err)
	}
	var pending int
	pendingRows := func() int {
		t.Helper()
		if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson WHERE bothdate IS NOT NULL").Scan(&pending); err != nil {
			t.Fatal(err)
		}
		return pending
	}
	scan, err := residentprofile.BackfillBirthdates(ctx, h.DB.DB, h.Box, 10, false)
	if err != nil || scan.Before != 2 || scan.Migrated != 0 || scan.Complete || pendingRows() != 2 {
		t.Fatalf("birthdate scan changed data or lost legacy dates: %+v %v", scan, err)
	}
	mustExec(t, h.DB, "UPDATE oldperson SET birthdate_cipher=?,birthdate_protected=true WHERE old_id=5", original)
	if report, err := residentprofile.BackfillBirthdates(ctx, h.DB.DB, h.Box, 10, true); err == nil || report.Migrated != 0 || pendingRows() != 2 {
		t.Fatalf("wrong-row ciphertext did not block all writes: %+v %v", report, err)
	}
	if got := callAPI(withHealthPrincipal(h, admin, h.listResidents), map[string]any{"name": ""}); got.Code != http.StatusInternalServerError {
		t.Fatal("list accepted a birthdate sealed for a different resident")
	}
	mustExec(t, h.DB, "DELETE FROM oldperson WHERE old_id=5")

	for index, want := range []int{1, 0, 0} {
		report, err := residentprofile.BackfillBirthdates(ctx, h.DB.DB, h.Box, 1, true)
		if (want == 1 && !errors.Is(err, residentprofile.ErrBirthdateBackfillIncomplete)) || (want == 0 && err != nil) ||
			report.Complete != (want == 0) || report.Remaining == nil || *report.Remaining != int64(want) || pendingRows() != want {
			t.Fatalf("step %d failed bounded resume: %+v %v", index, report, err)
		}
		if index == 2 && report.Migrated != 0 {
			t.Fatal("completed birthdates were rewritten by the backfill")
		}
	}
	list := api(t, withHealthPrincipal(h, admin, h.listResidents), map[string]any{"name": ""})["oldPeople"].([]any)
	if len(list) != 4 {
		t.Fatal("birthdate backfill changed the number of resident records")
	}
	dated, undated := list[1].(map[string]any), list[3].(map[string]any)
	birth := time.Date(1956, 2, 29, 0, 0, 0, 0, time.UTC)
	if dated["bothdate"] != "1956-02-29" || dated["age"] != float64(ageOn(birth, time.Now())) ||
		undated["bothdate"] != nil || undated["age"] != float64(73) {
		t.Fatal("protected date or stored age fallback changed")
	}
	if _, err := h.DB.ExecContext(ctx, "UPDATE oldperson SET bothdate='1951-01-01' WHERE old_id=1"); err == nil {
		t.Fatal("schema permitted a protected row to regain a plaintext date")
	}
	mustExec(t, h.DB, `CREATE TRIGGER reject_birthdate_update BEFORE UPDATE ON oldperson FOR EACH ROW
 BEGIN IF NOT(NEW.birthdate_cipher <=> OLD.birthdate_cipher) THEN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic birthdate write failure'; END IF; END`)
	body["oldId"], body["name"], body["bothdate"] = 1, "must roll back", "1951-01-01"
	if out := callAPI(withHealthPrincipal(h, admin, h.updateResident), body); out.Code != http.StatusInternalServerError {
		t.Fatal("rejected birthdate update was accepted")
	}
	var name, cipher string
	if err := h.DB.QueryRowContext(ctx, "SELECT name,birthdate_cipher FROM oldperson WHERE old_id=1").Scan(&name, &cipher); err != nil ||
		name == body["name"] || cipher != original {
		t.Fatal("birthdate write failure changed the original transaction", err)
	}
}
