package rekey

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

// Exercise ciphertext produced and consumed by the actual account code. A
// generic AEAD round-trip would miss a wrong row scope or an omitted registry.
func TestAdministratorPhoneRekeyMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	old, current, retired, _ := boxes(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	get := func(query string, args ...any) string {
		t.Helper()
		var result string
		if err := pool.QueryRowContext(ctx, query, args...).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	password := "SyntheticPassword123"
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = userphone.BindLookupKey(ctx, tx, old); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	phones := map[int64]string{1: "13800000001", 10: "13800000010"}
	lookup := map[int64]string{}
	for _, id := range []int64{1, 10} {
		protected, err := userphone.Protect(old, id, phones[id])
		if err != nil {
			t.Fatal(err)
		}
		lookup[id] = protected.Lookup
		exec(`INSERT INTO users(user_id,user_name,password,role,user_phone_cipher,user_phone_lookup,user_phone_protected)
 VALUES(?,'synthetic',?,'1',?,?,true)`, id, hash, protected.Ciphertext, protected.Lookup)
	}
	exec("INSERT INTO users(user_id,user_name,user_phone,user_phone_protected) VALUES(20,'legacy','13800000020',false),(30,'removed',NULL,true)")
	binding := get("SELECT lookup_verifier FROM account_phone_key_state WHERE singleton=1")
	exec("INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')")
	exec("INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'REKEY-ACCOUNT',?,1)", strings.Repeat("x", 48))
	exec("INSERT INTO care_incident(id,old_id,device_id,resolution_note) VALUES(1,1,1,?)", sealed(t, old, "synthetic note", "care-text"))
	options := Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 100}

	t.Run("rotation preserves actual login with only the new reader", func(t *testing.T) {
		for _, id := range []int64{1, 10} {
			match, err := authrepo.Authenticate(ctx, pool, old, phones[id], password)
			if err != nil || !match.Authenticated || match.Principal.ID != id {
				t.Fatal("old key fixture cannot authenticate")
			}
		}
		report, err := Run(ctx, pool.DB, current, old, options)
		if err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized {
			t.Fatal("registered field rotation failed", err)
		}
		found := false
		for _, column := range report.Applied {
			if column.Name == "users.user_phone_cipher" {
				found = column.Rewritten == 2 && column.Empty == 2 && column.Complete
			}
		}
		if !found {
			t.Fatal("administrator phone ciphertext was omitted from real rotation")
		}
		for _, id := range []int64{1, 10} {
			match, err := authrepo.Authenticate(ctx, pool, retired, phones[id], password)
			if err != nil || !match.Authenticated || match.Principal.ID != id || match.Principal.Phone != phones[id] {
				t.Fatal("administrator login still depends on the old reader")
			}
			if get("SELECT user_phone_lookup FROM users WHERE user_id=?", id) != lookup[id] ||
				get("SELECT CONCAT(user_phone IS NULL,':',user_phone_protected) FROM users WHERE user_id=?", id) != "1:1" {
				t.Fatal("rotation changed phone index or protection state")
			}
		}
		if get("SELECT lookup_verifier FROM account_phone_key_state WHERE singleton=1") != binding ||
			get("SELECT user_phone FROM users WHERE user_id=20") != "13800000020" ||
			get("SELECT user_phone_cipher IS NULL AND user_phone_lookup IS NULL FROM users WHERE user_id=30") != "1" {
			t.Fatal("rotation changed lookup binding, legacy data or removed phone")
		}
		encoded, err := json.Marshal(report)
		if err != nil || strings.Contains(string(encoded), phones[1]) || strings.Contains(string(encoded), lookup[1]) {
			t.Fatal("aggregate rotation report leaked account data")
		}
	})

	t.Run("row swaps fail preflight before earlier fields change", func(t *testing.T) {
		first, err := userphone.Protect(old, 1, phones[1])
		if err != nil {
			t.Fatal(err)
		}
		second, err := userphone.Protect(old, 10, phones[10])
		if err != nil {
			t.Fatal(err)
		}
		exec("UPDATE users SET user_phone_cipher=? WHERE user_id=1", second.Ciphertext)
		exec("UPDATE users SET user_phone_cipher=? WHERE user_id=10", first.Ciphertext)
		note := sealed(t, old, "must remain unchanged", "care-text")
		exec("UPDATE care_incident SET resolution_note=? WHERE id=1", note)
		report, err := Run(ctx, pool.DB, current, old, options)
		if err == nil || report.Stage != "preflight" || len(report.Applied) != 0 ||
			get("SELECT resolution_note FROM care_incident WHERE id=1") != note ||
			get("SELECT user_phone_cipher FROM users WHERE user_id=1") != second.Ciphertext {
			t.Fatal("wrong account scope did not block all writes")
		}
		exec("UPDATE users SET user_phone_cipher=? WHERE user_id=1", first.Ciphertext)
		exec("UPDATE users SET user_phone_cipher=? WHERE user_id=10", second.Ciphertext)
	})

	t.Run("CAS preserves concurrent phone replacement and index", func(t *testing.T) {
		var field target
		for _, candidate := range targets() {
			if candidate.name() == "users.user_phone_cipher" {
				field = candidate
			}
		}
		if field.table == "" {
			t.Fatal("phone target missing")
		}
		stale := get("SELECT user_phone_cipher FROM users WHERE user_id=1")
		fresh, err := userphone.Protect(current, 1, "13800000011")
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, err := pool.ExecContext(ctx, "UPDATE users SET user_phone_cipher=?,user_phone_lookup=? WHERE user_id=1", fresh.Ciphertext, fresh.Lookup)
			done <- err
		}()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		changed, err := compareSwap(ctx, pool.DB, field, []any{int64(1)}, stale, "must not win")
		if err != nil || changed || get("SELECT user_phone_cipher FROM users WHERE user_id=1") != fresh.Ciphertext ||
			get("SELECT user_phone_lookup FROM users WHERE user_id=1") != fresh.Lookup {
			t.Fatal("stale rotation overwrote a newer phone or its index")
		}
		match, err := authrepo.Authenticate(ctx, pool, retired, "13800000011", password)
		if err != nil || !match.Authenticated || match.Principal.ID != 1 {
			t.Fatal("concurrent replacement is not usable")
		}
	})
	t.Run("single key legacy phone keeps its original lookup key", testLegacyAdministratorPhoneRekey)
}

func testLegacyAdministratorPhoneRekey(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, _, _, legacy := boxes(t)
	key := func(n byte) string {
		return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string([]byte{n}), 32)))
	}
	current, err := securestore.NewKeyring("B", map[string]string{"A": key(1), "B": key(2)}, key(1))
	if err != nil {
		t.Fatal(err)
	}
	retired, err := securestore.NewKeyring("B", map[string]string{"B": key(2)}, key(1))
	if err != nil {
		t.Fatal(err)
	}
	phone := "13800000007"
	password := "SyntheticPassword123"
	protected, err := userphone.Protect(legacy, 7, phone)
	if err != nil || strings.HasPrefix(protected.Ciphertext, "sne1:v2:") {
		t.Fatal("fixture must use the unversioned production format")
	}
	hash, err := auth.Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := userphone.BindLookupKey(ctx, tx, legacy); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, `INSERT INTO users(user_id,user_name,password,role,user_phone_cipher,user_phone_lookup,user_phone_protected)
 VALUES(7,'synthetic',?,'1',?,?,true)`, hash, protected.Ciphertext, protected.Lookup); err != nil {
		t.Fatal(err)
	}
	before, err := authrepo.Authenticate(ctx, pool, legacy, phone, password)
	if err != nil || !before.Authenticated || before.Principal.ID != 7 {
		t.Fatal("legacy fixture cannot authenticate")
	}
	report, err := Run(ctx, pool.DB, current, legacy, Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 100})
	if err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized {
		t.Fatal("legacy phone migration failed", err)
	}
	found := false
	for _, column := range report.Before {
		if column.Name == "users.user_phone_cipher" {
			found = column.Legacy == 1
		}
	}
	if !found {
		t.Fatal("unversioned phone omitted from scan")
	}
	match, err := authrepo.Authenticate(ctx, pool, retired, phone, password)
	if err != nil || !match.Authenticated || match.Principal.ID != 7 || match.Principal.Phone != phone {
		t.Fatal("legacy phone needs old encryption reader or lookup key was changed")
	}
}
