package residentprofile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func execProfile(t *testing.T, pool *db.Database, query string, args ...any) {
	t.Helper()
	if _, err := pool.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func readProfile(t *testing.T, pool *db.Database, box *securestore.Box, id int64) Values {
	t.Helper()
	_, record, err := scanRecord(pool.QueryRowContext(context.Background(), "SELECT "+recordColumns+" FROM oldperson WHERE old_id=?", id))
	if err != nil {
		t.Fatal(err)
	}
	values, err := record.Open(box, id)
	if err != nil {
		t.Fatal("profile could not be authenticated", err)
	}
	return values
}

func TestResidentProfileBackfillMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := testBox(t, "A", "B")
	ctx := context.Background()
	execProfile(t, pool, `INSERT INTO oldperson(old_id,name,phone,room,illness,food_like,food_hate,drag)
 VALUES(1,'synthetic','13800000001','301','condition','rice','peanut','medication'),
 (2,'synthetic','13800000001 ','',NULL,NULL,'',NULL),(3,'unattributed',NULL,NULL,NULL,NULL,NULL,NULL)`)
	before := []Values{readProfile(t, pool, box, 1), readProfile(t, pool, box, 2), readProfile(t, pool, box, 3)}
	scan, err := Backfill(ctx, pool.DB, box, 1, false)
	if err != nil || scan.Before != 2 || scan.Migrated != 0 || scan.Complete {
		t.Fatal("scan changed profiles or lost inventory", err)
	}
	var bindings int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM resident_profile_key_state").Scan(&bindings); err != nil || bindings != 0 {
		t.Fatal("scan created a binding", err)
	}
	partial, err := Backfill(ctx, pool.DB, box, 1, true)
	if !errors.Is(err, ErrBackfillIncomplete) || partial.Migrated != 1 || partial.Remaining == nil || *partial.Remaining != 1 || partial.Complete {
		t.Fatal("bounded backfill accepted an incomplete run", err)
	}
	index, err := SearchIndex(ctx, pool, box, "13800000001 ")
	if err != nil {
		t.Fatal(err)
	}
	var matches int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM oldperson WHERE "+PhoneMatch, "13800000001 ", index).Scan(&matches); err != nil || matches != 2 {
		t.Fatal("mixed old/protected phones lost duplicate PAD SPACE matches", err)
	}
	for id := range 3 {
		if !reflect.DeepEqual(before[id], readProfile(t, pool, box, int64(id+1))) {
			t.Fatal("partial migration changed values or NULL/empty distinctions")
		}
	}
	report, err := Backfill(ctx, pool.DB, box, 100, true)
	if err != nil || !report.Complete || report.Migrated != 1 {
		t.Fatal("backfill could not resume", err)
	}
	report, err = Backfill(ctx, pool.DB, box, 100, true)
	if err != nil || !report.Complete || report.Migrated != 0 {
		t.Fatal("backfill was not idempotent", err)
	}
	for id := range 3 {
		if !reflect.DeepEqual(before[id], readProfile(t, pool, box, int64(id+1))) {
			t.Fatal("backfill changed existing profile values")
		}
	}
	var plaintext, count int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(phone)+COUNT(room)+COUNT(illness)+COUNT(warn)+COUNT(food_like)+COUNT(food_hate)+COUNT(drag),COUNT(*) FROM oldperson").Scan(&plaintext, &count); err != nil || plaintext != 0 || count != 3 {
		t.Fatal("migration lost rows or retained sensitive plaintext", err)
	}
	encoded, _ := json.Marshal(report)
	if string(encoded) == "" || containsSensitiveReport(encoded, "13800000001", "condition", "medication") {
		t.Fatal("aggregate report leaked profile fields")
	}
}

func containsSensitiveReport(payload []byte, values ...string) bool {
	for _, value := range values {
		if bytes.Contains(payload, []byte(value)) {
			return true
		}
	}
	return false
}

func TestResidentProfilePreflightMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := testBox(t, "A", "B")
	ctx := context.Background()
	execProfile(t, pool, "INSERT INTO oldperson(old_id,phone) VALUES(1,'13800000001')")
	protected, err := Protect(box, 99, Strings("13800000099", "", "late condition", "", "", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := PrepareWrite(ctx, tx, box); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO oldperson(old_id,care_profile_cipher,phone_lookup,care_profile_protected) VALUES(99,?,?,true)", protected.Cipher, protected.Lookup); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	execProfile(t, pool, "UPDATE oldperson SET care_profile_cipher='sne1:v2:A:***' WHERE old_id=99")
	if report, err := Backfill(ctx, pool.DB, box, 100, true); err == nil || report.Migrated != 0 || report.Complete {
		t.Fatal("late authentication failure allowed an earlier legacy rewrite")
	}
	var raw string
	if err := pool.QueryRowContext(ctx, "SELECT phone FROM oldperson WHERE old_id=1").Scan(&raw); err != nil || raw != "13800000001" {
		t.Fatal("valid earlier legacy row changed before preflight finished", err)
	}
	execProfile(t, pool, "UPDATE oldperson SET care_profile_cipher=? WHERE old_id=99", protected.Cipher)
	if _, err := SearchIndex(ctx, pool, testBox(t, "A", "C"), raw); err == nil {
		t.Fatal("wrong lookup key was accepted")
	}
	execProfile(t, pool, "DELETE FROM resident_profile_key_state")
	if err := VerifyKey(ctx, pool, box); err == nil {
		t.Fatal("missing ledger on protected profiles was ignored")
	}
	if report, err := Backfill(ctx, pool.DB, box, 100, true); err == nil || report.Migrated != 0 {
		t.Fatal("backfill silently rebound a missing ledger")
	}
	var bindings int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM resident_profile_key_state").Scan(&bindings); err != nil || bindings != 0 {
		t.Fatal("failed verification inserted a replacement binding", err)
	}
	for _, statement := range []string{
		"UPDATE oldperson SET phone='13800000099' WHERE old_id=99",
		"UPDATE oldperson SET care_profile_protected=false WHERE old_id=99",
		"UPDATE oldperson SET phone_lookup='bad' WHERE old_id=99",
	} {
		if _, err := pool.ExecContext(ctx, statement); err == nil {
			t.Fatal("database accepted inconsistent profile metadata")
		}
	}
}

func TestResidentProfileBackfillLocksMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := testBox(t, "A", "B")
	ctx := context.Background()
	execProfile(t, pool, "INSERT INTO oldperson(old_id,phone,illness) VALUES(1,'13800000001','original')")
	lock, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var id int64
	if err := lock.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if changed, err := backfillOne(blocked, pool.DB, box); err == nil || changed {
		t.Fatal("backfill bypassed the row lock or accepted a canceled mutation")
	}
	if _, err := lock.ExecContext(ctx, "UPDATE oldperson SET illness='new committed value' WHERE old_id=1"); err != nil {
		t.Fatal(err)
	}
	if err := lock.Commit(); err != nil {
		t.Fatal(err)
	}
	if report, err := Backfill(ctx, pool.DB, box, 1, true); err != nil || !report.Complete || report.Migrated != 1 {
		t.Fatal("canceled migration could not resume", err)
	}
	values := readProfile(t, pool, box, 1)
	if values.Illness == nil || *values.Illness != "new committed value" {
		t.Fatal("backfill overwrote a writer with an old scanned value")
	}
}
