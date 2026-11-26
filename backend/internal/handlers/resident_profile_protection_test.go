package handlers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/rekey"
	"yihe-care/internal/residentprofile"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

// Exercise actual profile writers and every known consumer of the contact
// phone. A trailing ASCII space in the family lookup deliberately checks the
// MySQL PAD SPACE equality used by the existing binding query.
func TestResidentCareProfileProtectedMySQL(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	var err error
	h.Box, err = securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("R", 32))))
	if err != nil {
		t.Fatal(err)
	}
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'synthetic guardian')")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	body := elderPayload()
	body["phone"], body["room"], body["illness"], body["warn"] = "13800000000", "A栋301室", "synthetic condition", "synthetic caution"
	body["foodLike"], body["foodHate"], body["drag"] = "米饭", "花生", "synthetic medication"
	if out := api(t, withHealthPrincipal(h, admin, h.createResident), body); out["returnCode"] != "1111" {
		t.Fatal("resident creation failed")
	}
	columns := []string{"phone", "room", "illness", "warn", "food_like", "food_hate", "drag", "bothdate"}
	for _, column := range columns {
		var raw sql.NullString
		if err := pool.QueryRowContext(context.Background(), "SELECT "+column+" FROM oldperson WHERE old_id=1").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if raw.Valid {
			t.Errorf("protected resident field remains in plaintext column %s", column)
		}
	}
	for _, endpoint := range []struct {
		fn  http.HandlerFunc
		key string
	}{{h.listResidents, "oldPeople"}, {h.getResidentDetails, "oldPersonDetails"}} {
		out := api(t, withHealthPrincipal(h, admin, endpoint.fn), map[string]any{"name": ""})
		items, ok := out[endpoint.key].([]any)
		if !ok || len(items) != 1 {
			t.Fatal("profile read model changed")
		}
		item := items[0].(map[string]any)
		for _, field := range []string{"phone", "room", "illness", "warn", "foodLike", "foodHate", "drag", "bothdate"} {
			if item[field] != body[field] {
				t.Errorf("authorized profile field changed: %s", field)
			}
		}
	}
	family := map[string]any{"familyName": "家属", "familyRelation": "子女", "familySex": "女", "familyPhone": "13800000003", "oldPhone": "13800000000 ", "familyAddress": "synthetic address"}
	if out := api(t, withHealthPrincipal(h, admin, h.familyAdd), family); out["returnCode"] != "1111" {
		t.Fatal("protected phone no longer supports existing PAD SPACE family lookup")
	}
	mustExec(t, pool, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'PROFILE-LAB','unused',1)")
	out := api(t, withHealthPrincipal(h, admin, h.deviceList), map[string]any{"query": "", "state": "all"})
	devices, ok := out["devices"].([]any)
	if !ok || len(devices) != 1 || devices[0].(map[string]any)["oldPersonPhone"] != body["phone"] {
		t.Fatal("device list lost the protected resident phone")
	}
}

func residentProfileFixture(t *testing.T) (*Handler, session.Principal, map[string]any) {
	t.Helper()
	pool := testDatabase(t)
	h := testHandler(t, pool)
	var err error
	h.Box, err = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("R")}, residentTestKey("C"))
	if err != nil {
		t.Fatal(err)
	}
	seedCareAdminDuty(t, pool, 11, "supervisor")
	mustExec(t, pool, "INSERT INTO customer(customer_id,customer_name) VALUES(1,'synthetic guardian')")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	body := elderPayload()
	body["illness"], body["foodLike"], body["foodHate"], body["drag"] = "synthetic condition", "rice", "peanut", "synthetic medication"
	if out := api(t, withHealthPrincipal(h, admin, h.createResident), body); out["returnCode"] != "1111" {
		t.Fatal("cannot seed profile through the actual writer")
	}
	return h, admin, body
}

func residentTestKey(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32)))
}

func residentFamilyInput() map[string]any {
	return map[string]any{"familyName": "家属", "familyRelation": "子女", "familySex": "女", "familyPhone": "13800000003", "oldPhone": "13800000000", "familyAddress": "synthetic address"}
}

func TestResidentProfileAtomicFailureMySQL(t *testing.T) {
	h, admin, body := residentProfileFixture(t)
	ctx := context.Background()
	var cipher, name string
	if err := h.DB.QueryRowContext(ctx, "SELECT care_profile_cipher,name FROM oldperson WHERE old_id=1").Scan(&cipher, &name); err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func() {
		t.Helper()
		var currentCipher, currentName string
		var rows int
		if err := h.DB.QueryRowContext(ctx, "SELECT care_profile_cipher,name,(SELECT COUNT(*) FROM oldperson) FROM oldperson WHERE old_id=1").Scan(&currentCipher, &currentName, &rows); err != nil || currentCipher != cipher || currentName != name || rows != 1 {
			t.Fatal("failed profile write changed the original transaction", err)
		}
	}
	mustExec(t, h.DB, `CREATE TRIGGER reject_resident_profile BEFORE UPDATE ON oldperson FOR EACH ROW
 BEGIN IF NOT(NEW.care_profile_cipher <=> OLD.care_profile_cipher) THEN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic protection failure'; END IF; END`)
	body["oldId"], body["name"] = 1, "must roll back"
	for _, writer := range []http.HandlerFunc{h.updateResident, h.createResident} {
		if out := callAPI(withHealthPrincipal(h, admin, writer), body); out.Code != http.StatusInternalServerError {
			t.Fatal("failed sealing persistence was accepted")
		}
		assertUnchanged()
	}
	mustExec(t, h.DB, "DROP TRIGGER reject_resident_profile")
	originalBox := h.Box
	for _, scenario := range []string{"missing key", "wrong encryption key", "wrong lookup key"} {
		switch scenario {
		case "missing key":
			h.Box = nil
		case "wrong encryption key":
			h.Box, _ = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("S")}, residentTestKey("C"))
		case "wrong lookup key":
			h.Box, _ = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("R")}, residentTestKey("D"))
		}
		if out := callAPI(withHealthPrincipal(h, admin, h.updateResident), body); out.Code != http.StatusInternalServerError {
			t.Fatal("unverified key rewrote an existing protected profile")
		}
		assertUnchanged()
	}
	h.Box = originalBox
	mustExec(t, h.DB, "UPDATE oldperson SET care_profile_cipher='sne1:v2:A:***' WHERE old_id=1")
	mustExec(t, h.DB, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'PROFILE-FAILURE','unused',1)")
	for _, endpoint := range []struct {
		fn   http.HandlerFunc
		body map[string]any
	}{{h.listResidents, map[string]any{"name": ""}}, {h.getResidentDetails, map[string]any{"name": ""}},
		{h.deviceList, map[string]any{"query": "", "state": "all"}}, {h.updateResident, body}, {h.familyAdd, residentFamilyInput()}} {
		if out := callAPI(withHealthPrincipal(h, admin, endpoint.fn), endpoint.body); out.Code != http.StatusInternalServerError {
			t.Fatal("corrupt profile bypassed a consumer's authentication")
		}
	}
	var families int
	if err := h.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM family").Scan(&families); err != nil || families != 0 {
		t.Fatal("failed identity authentication left a new family", err)
	}
}

func TestResidentProfilePhoneAmbiguityMySQL(t *testing.T) {
	h, admin, _ := residentProfileFixture(t)
	mustExec(t, h.DB, "INSERT INTO oldperson(old_id,name,phone,room) VALUES(2,'legacy duplicate','13800000000 ','legacy room')")
	for iteration := range 2 {
		out := callAPI(withHealthPrincipal(h, admin, h.familyAdd), residentFamilyInput())
		if out.Code != http.StatusConflict {
			t.Fatal("duplicate resident phone selected an arbitrary owner")
		}
		if iteration == 0 {
			if report, err := residentprofile.Backfill(context.Background(), h.DB.DB, h.Box, 100, true); err != nil || !report.Complete || report.Migrated != 1 {
				t.Fatal("duplicate phone backfill changed legitimate ambiguity", err)
			}
		}
	}
	var families, assigned int
	if err := h.DB.QueryRowContext(context.Background(), "SELECT (SELECT COUNT(*) FROM family),(SELECT COUNT(*) FROM oldperson WHERE family_id IS NOT NULL)").Scan(&families, &assigned); err != nil || families != 0 || assigned != 0 {
		t.Fatal("ambiguous family association changed ownership", err)
	}
}

func TestResidentProfileRotationMySQL(t *testing.T) {
	h, admin, body := residentProfileFixture(t)
	ctx := context.Background()
	old := h.Box
	current, err := securestore.NewKeyring("B", map[string]string{"A": residentTestKey("R"), "B": residentTestKey("S")}, residentTestKey("C"))
	if err != nil {
		t.Fatal(err)
	}
	reader, err := securestore.NewKeyring("B", map[string]string{"B": residentTestKey("S")}, residentTestKey("C"))
	if err != nil {
		t.Fatal(err)
	}
	var beforeLookup, beforeBinding string
	if err := h.DB.QueryRowContext(ctx, "SELECT phone_lookup,(SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1) FROM oldperson WHERE old_id=1").Scan(&beforeLookup, &beforeBinding); err != nil {
		t.Fatal(err)
	}
	if report, err := rekey.Run(ctx, h.DB.DB, current, old, rekey.Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 1000}); err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized {
		t.Fatal("profile rotation failed or authorized full asset retirement", err)
	}
	var lookup, binding string
	if err := h.DB.QueryRowContext(ctx, "SELECT phone_lookup,(SELECT lookup_verifier FROM resident_profile_key_state WHERE singleton=1) FROM oldperson WHERE old_id=1").Scan(&lookup, &binding); err != nil || lookup != beforeLookup || binding != beforeBinding {
		t.Fatal("rotation changed phone lookup identity", err)
	}
	h.Box = reader
	for _, endpoint := range []struct {
		fn  http.HandlerFunc
		key string
	}{{h.listResidents, "oldPeople"}, {h.getResidentDetails, "oldPersonDetails"}} {
		out := api(t, withHealthPrincipal(h, admin, endpoint.fn), map[string]any{"name": ""})
		items, ok := out[endpoint.key].([]any)
		if !ok || len(items) != 1 || items[0].(map[string]any)["phone"] != body["phone"] ||
			items[0].(map[string]any)["drag"] != body["drag"] || items[0].(map[string]any)["bothdate"] != body["bothdate"] {
			t.Fatal("production profile reader still needs the old key")
		}
	}
	h.Box = old
	if out := callAPI(withHealthPrincipal(h, admin, h.listResidents), map[string]any{"name": ""}); out.Code != http.StatusInternalServerError {
		t.Fatal("old-only reader opened rotated fields")
	}
	h.Box = reader
	if out := api(t, withHealthPrincipal(h, admin, h.familyAdd), residentFamilyInput()); out["returnCode"] != "1111" {
		t.Fatal("rotated phone no longer supports family association")
	}
	body["oldId"], body["phone"], body["illness"], body["bothdate"] = 1, "010-12345678", "updated condition", "1956-02-29"
	if out := api(t, withHealthPrincipal(h, admin, h.updateResident), body); out["returnCode"] != "1111" {
		t.Fatal("rotated profile could not be edited")
	}
	edited := api(t, withHealthPrincipal(h, admin, h.listResidents), map[string]any{"name": ""})
	if edited["oldPeople"].([]any)[0].(map[string]any)["bothdate"] != body["bothdate"] {
		t.Fatal("the edited birthdate was not restored")
	}
	mustExec(t, h.DB, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'PROFILE-ROTATED','unused',1)")
	out := api(t, withHealthPrincipal(h, admin, h.deviceList), map[string]any{"query": "", "state": "all"})
	devices, ok := out["devices"].([]any)
	if !ok || len(devices) != 1 || devices[0].(map[string]any)["oldPersonPhone"] != body["phone"] {
		t.Fatal("device view lost the edited rotated contact")
	}
}
