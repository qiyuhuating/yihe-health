package userphone_test

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/authrepo"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/rekey"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

func TestFamilyPhoneBackfillMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	key := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(s, 32))) }
	old, err := securestore.NewKeyring("A", map[string]string{"A": key("a")}, key("c"))
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	get := func(query string, args ...any) string {
		t.Helper()
		var out string
		if err := pool.QueryRowContext(ctx, query, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	hash, err := auth.Hash("SyntheticPassword123")
	if err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO family(family_id,family_name,family_phone,family_password) VALUES(1,'one','13800000001',?),(10,'ten','13800000010',?),(20,'twenty','invalid',?),(30,'none',NULL,NULL)", hash, hash, hash)
	exec("INSERT INTO oldperson(old_id,name,family_id) VALUES(1,'one',1),(10,'ten',10),(20,'twenty',20)")
	scan, err := userphone.BackfillFamily(ctx, pool.DB, nil, 1, false)
	if err != nil || scan.Before != 3 || scan.Migrated != 0 || scan.Complete || get("SELECT family_phone FROM family WHERE family_id=1") != "13800000001" {
		t.Fatal("readonly scan changed legacy data or reported completion")
	}
	first, err := userphone.BackfillFamily(ctx, pool.DB, old, 1, true)
	if !errors.Is(err, userphone.ErrFamilyBackfillIncomplete) || first.Migrated != 1 || first.Remaining == nil || *first.Remaining != 2 || first.Complete {
		t.Fatal("bounded backfill hid its partial result", err)
	}
	before := get("SELECT family_phone_cipher FROM family WHERE family_id=1")
	if get("SELECT family_phone IS NULL AND family_phone_protected=true FROM family WHERE family_id=1") != "1" || strings.Contains(before, "13800000001") {
		t.Fatal("backfill retained a plaintext primary phone")
	}
	wrongLookup, err := securestore.NewKeyring("A", map[string]string{"A": key("a")}, key("d"))
	if err != nil {
		t.Fatal(err)
	}
	wrongCipher, err := securestore.NewKeyring("A", map[string]string{"A": key("z")}, key("c"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*securestore.Box{wrongLookup, wrongCipher} {
		report, err := userphone.BackfillFamily(ctx, pool.DB, bad, 100, true)
		if err == nil || report.Migrated != 0 || report.Complete || get("SELECT family_phone FROM family WHERE family_id=10") != "13800000010" {
			t.Fatal("wrong lookup/encryption key allowed a legacy write")
		}
	}
	// Invalid historical data must stop at that row without undoing earlier
	// committed rows. The operator corrects it inside the stopped-writer drill.
	partial, err := userphone.BackfillFamily(ctx, pool.DB, old, 100, true)
	if err == nil || partial.Migrated != 1 || partial.Complete || get("SELECT family_phone FROM family WHERE family_id=20") != "invalid" {
		t.Fatal("invalid legacy input was silently protected or hidden")
	}
	exec("UPDATE family SET family_phone='13800000020' WHERE family_id=20")
	final, err := userphone.BackfillFamily(ctx, pool.DB, old, 100, true)
	if err != nil || final.Migrated != 1 || !final.Complete || final.Remaining == nil || *final.Remaining != 0 {
		t.Fatal("restartable family backfill did not finish", err)
	}
	again, err := userphone.BackfillFamily(ctx, pool.DB, old, 100, true)
	if err != nil || again.Migrated != 0 || !again.Complete || get("SELECT family_phone_cipher FROM family WHERE family_id=1") != before {
		t.Fatal("idempotent backfill rewrote an existing cipher")
	}
	for _, bad := range []*securestore.Box{nil, wrongCipher, wrongLookup} {
		if _, err := authrepo.Authenticate(ctx, pool, bad, "13800000001", "SyntheticPassword123"); err == nil {
			t.Fatal("protected family authentication fell back to plaintext or accepted a wrong key")
		}
	}
	current, err := securestore.NewKeyring("B", map[string]string{"A": key("a"), "B": key("b")}, key("c"))
	if err != nil {
		t.Fatal(err)
	}
	retired, err := securestore.NewKeyring("B", map[string]string{"B": key("b")}, key("c"))
	if err != nil {
		t.Fatal(err)
	}
	index := get("SELECT family_phone_lookup FROM family WHERE family_id=1")
	report, err := rekey.Run(ctx, pool.DB, current, old, rekey.Options{Apply: true, BatchSize: 1, MaxRowsPerColumn: 100})
	if err != nil || !report.EncryptedColumnsClean || report.RetirementAuthorized || get("SELECT family_phone_lookup FROM family WHERE family_id=1") != index {
		t.Fatal("family rotation failed or changed the exact lookup identity", err)
	}
	match, err := authrepo.Authenticate(ctx, pool, retired, "13800000001", "SyntheticPassword123")
	if err != nil || !match.Authenticated || match.Principal.ID != 1 || match.Principal.OldID != 1 {
		t.Fatal("migrated family no longer authenticates with only the new reader", err)
	}
	exec("INSERT INTO family(family_id,family_phone,family_phone_cipher) VALUES(40,'13800000040',?)", before)
	invalid, err := userphone.BackfillFamily(ctx, pool.DB, retired, 100, true)
	if err == nil || invalid.Migrated != 0 || invalid.Complete || get("SELECT family_phone_cipher FROM family WHERE family_id=40") != before {
		t.Fatal("backfill overwrote inconsistent legacy metadata")
	}
	exec("DELETE FROM family WHERE family_id=40")
	exec("DELETE FROM account_phone_key_state")
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = userphone.BindLookupKey(ctx, tx, retired)
	tx.Rollback()
	if err == nil || get("SELECT COUNT(*) FROM account_phone_key_state") != "0" {
		t.Fatal("missing binding for protected rows was silently recreated")
	}
}
