package handlers

import (
	"context"
	"database/sql"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yihe-care/internal/auth"
	"yihe-care/internal/rekey"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func TestGuardianContactProtectedMySQL(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	var err error
	h.Box, err = securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("G", 32))))
	if err != nil {
		t.Fatal(err)
	}
	seedCareAdminDuty(t, pool, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	body := map[string]any{"customerName": "synthetic guardian", "customerAge": "40", "customerSex": "女",
		"customerPhone": "0731-1234567", "customerAddress": "synthetic contact address"}
	body["customerId"] = 999
	if out := apiFailure(t, http.StatusNotFound, withHealthPrincipal(h, admin, h.updateGuardian), body); out["code"] != "NOT_FOUND" || out["returnCode"] == "1111" {
		t.Fatal("missing guardian update was accepted")
	}
	var markers int
	if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM guardian_contact_key_state").Scan(&markers); err != nil || markers != 0 {
		t.Fatal("missing guardian update committed key metadata", err)
	}
	if out := api(t, withHealthPrincipal(h, admin, h.createGuardian), body); out["returnCode"] != "1111" {
		t.Fatal("guardian creation failed")
	}
	var id int64
	if err := pool.QueryRowContext(context.Background(), "SELECT customer_id FROM customer").Scan(&id); err != nil {
		t.Fatal(err)
	}
	check := func(stage string) {
		t.Helper()
		var phone, address sql.NullString
		if err := pool.QueryRowContext(context.Background(), "SELECT customer_phone,customer_address FROM customer WHERE customer_id=?", id).Scan(&phone, &address); err != nil {
			t.Fatal(err)
		}
		if phone.Valid {
			t.Errorf("%s retained guardian phone in plaintext", stage)
		}
		if address.Valid {
			t.Errorf("%s retained guardian address in plaintext", stage)
		}
		out := api(t, withHealthPrincipal(h, admin, h.listGuardians), map[string]any{"customerName": "synthetic", "page": 1, "pageSize": 20})
		items, ok := out["customers"].([]any)
		if !ok || len(items) != 1 {
			t.Fatal("guardian list contract changed")
		}
		item := items[0].(map[string]any)
		if item["customerPhone"] != body["customerPhone"] || item["customerAddress"] != body["customerAddress"] || item["customerName"] != body["customerName"] {
			t.Fatal("authorized guardian fields changed")
		}
	}
	check("create")
	body["customerId"], body["customerPhone"], body["customerAddress"] = id, "13800000002", strings.Repeat("地", 200)
	if out := api(t, withHealthPrincipal(h, admin, h.updateGuardian), body); out["returnCode"] != "1111" {
		t.Fatal("guardian update failed")
	}
	check("update")
}

func guardianContactFixture(t *testing.T) (*Handler, session.Principal, map[string]any) {
	t.Helper()
	pool := testDatabase(t)
	h := testHandler(t, pool)
	var err error
	h.Box, err = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("G")}, residentTestKey("C"))
	if err != nil {
		t.Fatal(err)
	}
	seedCareAdminDuty(t, pool, 11, "supervisor")
	admin := session.Principal{Role: auth.RoleAdmin, ID: 11}
	body := map[string]any{"customerName": "synthetic guardian", "customerAge": "40", "customerSex": "女",
		"customerPhone": "13800000001", "customerAddress": "synthetic address"}
	if out := api(t, withHealthPrincipal(h, admin, h.createGuardian), body); out["returnCode"] != "1111" {
		t.Fatal("cannot seed protected guardian")
	}
	body["customerId"] = 1
	return h, admin, body
}

func TestGuardianContactAtomicFailureMySQL(t *testing.T) {
	h, admin, body := guardianContactFixture(t)
	ctx := context.Background()
	var originalCipher, originalMarker string
	if err := h.DB.QueryRowContext(ctx, "SELECT contact_cipher,(SELECT key_verifier FROM guardian_contact_key_state WHERE singleton=1) FROM customer WHERE customer_id=1").Scan(&originalCipher, &originalMarker); err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func() {
		t.Helper()
		var cipher, marker, name string
		var count int
		if err := h.DB.QueryRowContext(ctx, "SELECT contact_cipher,customer_name,(SELECT COUNT(*) FROM customer),(SELECT key_verifier FROM guardian_contact_key_state WHERE singleton=1) FROM customer WHERE customer_id=1").Scan(&cipher, &name, &count, &marker); err != nil || cipher != originalCipher || marker != originalMarker || name != "synthetic guardian" || count != 1 {
			t.Fatal("failed guardian transaction changed stored state", err)
		}
	}
	body["customerName"] = "must roll back"
	mustExec(t, h.DB, `CREATE TRIGGER reject_guardian_contact BEFORE UPDATE ON customer FOR EACH ROW
 BEGIN IF NOT(NEW.contact_cipher <=> OLD.contact_cipher) THEN
 SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic contact failure'; END IF; END`)
	for _, writer := range []http.HandlerFunc{h.createGuardian, h.updateGuardian} {
		if out := callAPI(withHealthPrincipal(h, admin, writer), body); out.Code != http.StatusInternalServerError {
			t.Fatal("failed contact persistence was accepted")
		}
		assertUnchanged()
	}
	mustExec(t, h.DB, "DROP TRIGGER reject_guardian_contact")
	originalBox := h.Box
	for _, scenario := range []string{"missing", "wrong AEAD", "wrong binding"} {
		switch scenario {
		case "missing":
			h.Box = nil
		case "wrong AEAD":
			h.Box, _ = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("H")}, residentTestKey("C"))
		case "wrong binding":
			h.Box, _ = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("G")}, residentTestKey("D"))
		}
		for _, writer := range []http.HandlerFunc{h.createGuardian, h.updateGuardian} {
			if out := callAPI(withHealthPrincipal(h, admin, writer), body); out.Code != http.StatusInternalServerError {
				t.Fatal("unverified key created or overwrote guardian contacts")
			}
			assertUnchanged()
		}
		if out := callAPI(withHealthPrincipal(h, admin, h.listGuardians), map[string]any{"customerName": "no matches"}); out.Code != http.StatusInternalServerError {
			t.Fatal("empty page bypassed key verification")
		}
	}
	h.Box = originalBox
	mustExec(t, h.DB, "UPDATE customer SET contact_cipher='sne1:v2:A:***' WHERE customer_id=1")
	if out := callAPI(withHealthPrincipal(h, admin, h.updateGuardian), body); out.Code != http.StatusInternalServerError {
		t.Fatal("corrupted contact was silently overwritten")
	}
	mustExec(t, h.DB, "UPDATE customer SET contact_cipher=? WHERE customer_id=1", originalCipher)
	assertUnchanged()
}

func TestGuardianContactRotationMySQL(t *testing.T) {
	h, admin, body := guardianContactFixture(t)
	ctx := context.Background()
	old := h.Box
	current, _ := securestore.NewKeyring("B", map[string]string{"A": residentTestKey("G"), "B": residentTestKey("H")}, residentTestKey("C"))
	retired, _ := securestore.NewKeyring("B", map[string]string{"B": residentTestKey("H")}, residentTestKey("C"))
	h.Box = current
	second := map[string]any{}
	for key, value := range body {
		second[key] = value
	}
	second["customerName"] = "synthetic second"
	if out := api(t, withHealthPrincipal(h, admin, h.createGuardian), second); out["returnCode"] != "1111" {
		t.Fatal("new active key could not coexist with the old reader")
	}
	var binding string
	if err := h.DB.QueryRowContext(ctx, "SELECT lookup_verifier FROM guardian_contact_key_state WHERE singleton=1").Scan(&binding); err != nil {
		t.Fatal(err)
	}
	report, err := rekey.Run(ctx, h.DB.DB, current, old, rekey.Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 1000})
	if err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized {
		t.Fatal("contact/reader-marker rotation failed", err)
	}
	h.Box = retired
	out := api(t, withHealthPrincipal(h, admin, h.listGuardians), map[string]any{"customerName": "synthetic"})
	items, ok := out["customers"].([]any)
	if !ok || len(items) != 2 || items[0].(map[string]any)["customerPhone"] != body["customerPhone"] {
		t.Fatal("B-only real guardian list lost old contact data")
	}
	body["customerAddress"] = "updated with B-only reader"
	if out := api(t, withHealthPrincipal(h, admin, h.updateGuardian), body); out["returnCode"] != "1111" {
		t.Fatal("B-only real guardian update failed")
	}
	var after string
	if err := h.DB.QueryRowContext(ctx, "SELECT lookup_verifier FROM guardian_contact_key_state WHERE singleton=1").Scan(&after); err != nil || after != binding {
		t.Fatal("AEAD rotation changed the independent key binding", err)
	}
}

func TestGuardianContactFirstWritersMySQL(t *testing.T) {
	pool := testDatabase(t)
	h := testHandler(t, pool)
	h.Box, _ = securestore.NewKeyring("A", map[string]string{"A": residentTestKey("G")}, residentTestKey("C"))
	seedCareAdminDuty(t, pool, 11, "supervisor")
	create := withHealthPrincipal(h, session.Principal{Role: auth.RoleAdmin, ID: 11}, h.createGuardian)
	for range 5 {
		mustExec(t, pool, "DELETE FROM customer")
		mustExec(t, pool, "DELETE FROM guardian_contact_key_state")
		start := make(chan struct{})
		results := make(chan *httptest.ResponseRecorder, 2)
		for range 2 {
			go func() {
				<-start
				results <- callAPI(create, map[string]any{"customerName": "concurrent guardian", "customerAge": "40", "customerSex": "女", "customerPhone": "13800000001", "customerAddress": "synthetic address"})
			}()
		}
		close(start)
		for range 2 {
			out := <-results
			if out.Code != http.StatusOK || architectureBody(t, out)["returnCode"] != "1111" {
				t.Fatal("concurrent first contact writers did not both commit")
			}
		}
		var rows, protected, plaintext, markers int
		if err := pool.QueryRowContext(context.Background(), "SELECT COUNT(*),SUM(contact_protected),COUNT(customer_phone)+COUNT(customer_address),(SELECT COUNT(*) FROM guardian_contact_key_state) FROM customer").Scan(&rows, &protected, &plaintext, &markers); err != nil || rows != 2 || protected != 2 || plaintext != 0 || markers != 1 {
			t.Fatal("concurrent first writers split key state or lost contacts", err)
		}
	}
}
