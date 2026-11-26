package guardiancontact

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func execContact(t *testing.T, pool *db.Database, query string, args ...any) {
	t.Helper()
	if _, err := pool.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestGuardianContactBackfillMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := contactBox(t, "G", "C")
	ctx := context.Background()
	execContact(t, pool, `INSERT INTO customer(customer_id,customer_name,customer_phone,customer_address)
 VALUES(1,'synthetic','010-12345678','地'),(2,'synthetic','010-12345678',''),(3,'empty',NULL,NULL)`)
	before := []Values{}
	read := func(id int64) Values {
		t.Helper()
		_, record, err := scanRecord(pool.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM customer WHERE customer_id=?", id))
		if err != nil {
			t.Fatal(err)
		}
		values, err := record.Open(box, id)
		if err != nil {
			t.Fatal(err)
		}
		return values
	}
	for id := int64(1); id <= 3; id++ {
		before = append(before, read(id))
	}
	scan, err := Backfill(ctx, pool.DB, box, 1, false)
	if err != nil || scan.Migrated != 0 || scan.Before != 2 || scan.Complete {
		t.Fatal("scan changed contacts or lost pending count", err)
	}
	var markers int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM guardian_contact_key_state").Scan(&markers); err != nil || markers != 0 {
		t.Fatal("scan created key state", err)
	}
	partial, err := Backfill(ctx, pool.DB, box, 1, true)
	if !errors.Is(err, ErrBackfillIncomplete) || partial.Migrated != 1 || partial.Complete || partial.Remaining == nil || *partial.Remaining != 1 {
		t.Fatal("bounded apply hid partial progress", err)
	}
	complete, err := Backfill(ctx, pool.DB, box, 100, true)
	if err != nil || !complete.Complete || complete.Migrated != 1 {
		t.Fatal("backfill could not resume", err)
	}
	again, err := Backfill(ctx, pool.DB, box, 100, true)
	if err != nil || !again.Complete || again.Migrated != 0 {
		t.Fatal("backfill was not idempotent", err)
	}
	for id := int64(1); id <= 3; id++ {
		if !reflect.DeepEqual(before[id-1], read(id)) {
			t.Fatal("backfill changed duplicates, NULL or empty values")
		}
	}
	var plaintext, count int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(customer_phone)+COUNT(customer_address),COUNT(*) FROM customer").Scan(&plaintext, &count); err != nil || plaintext != 0 || count != 3 {
		t.Fatal("backfill retained contact plaintext or lost rows", err)
	}
	payload, _ := json.Marshal(complete)
	if strings.Contains(string(payload), "010-12345678") {
		t.Fatal("aggregate report leaked contact data")
	}
}

func TestGuardianContactPreflightMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := contactBox(t, "G", "C")
	ctx := context.Background()
	execContact(t, pool, "INSERT INTO customer(customer_id,customer_phone) VALUES(1,'13800000001')")
	if _, err := Backfill(ctx, pool.DB, box, 10, true); err != nil {
		t.Fatal(err)
	}
	execContact(t, pool, "INSERT INTO customer(customer_id,customer_phone) VALUES(2,'13800000002')")
	var cipher, binding, marker string
	if err := pool.QueryRowContext(ctx, "SELECT contact_cipher,(SELECT lookup_verifier FROM guardian_contact_key_state),(SELECT key_verifier FROM guardian_contact_key_state) FROM customer WHERE customer_id=1").Scan(&cipher, &binding, &marker); err != nil {
		t.Fatal(err)
	}
	assertBlocked := func(reader *securestore.Box) {
		t.Helper()
		report, err := Backfill(ctx, pool.DB, reader, 100, true)
		if err == nil || report.Migrated != 0 {
			t.Fatal("failed preflight migrated a contact")
		}
		var pending string
		if err := pool.QueryRowContext(ctx, "SELECT customer_phone FROM customer WHERE customer_id=2").Scan(&pending); err != nil || pending != "13800000002" {
			t.Fatal("failed preflight changed another legacy row", err)
		}
	}
	assertBlocked(contactBox(t, "H", "C"))
	assertBlocked(contactBox(t, "G", "D"))
	execContact(t, pool, "UPDATE customer SET contact_cipher='sne1:v2:A:***' WHERE customer_id=1")
	assertBlocked(box)
	execContact(t, pool, "UPDATE customer SET contact_cipher=? WHERE customer_id=1", cipher)
	execContact(t, pool, "UPDATE guardian_contact_key_state SET key_verifier='sne1:v2:A:***'")
	assertBlocked(box)
	execContact(t, pool, "UPDATE guardian_contact_key_state SET key_verifier=?", marker)
	execContact(t, pool, "DELETE FROM guardian_contact_key_state")
	assertBlocked(box)
	execContact(t, pool, "INSERT INTO guardian_contact_key_state(singleton,lookup_verifier,key_verifier) VALUES(1,?,?)", binding, marker)
	if _, err := pool.ExecContext(ctx, "UPDATE customer SET customer_address='mixed plaintext' WHERE customer_id=1"); err == nil {
		t.Fatal("database accepted mixed protected/plain contact values")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if report, err := Backfill(cancelled, pool.DB, box, 10, true); err == nil || report.Migrated != 0 {
		t.Fatal("cancelled preflight wrote contacts")
	}
}

func TestGuardianContactBackfillLocksMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := contactBox(t, "G", "C")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execContact(t, pool, "INSERT INTO customer(customer_id,customer_phone,customer_address) VALUES(1,'13800000001','old address')")
	metadata, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Rollback()
	if err := PrepareWrite(ctx, metadata, box); err != nil {
		t.Fatal(err)
	}
	if err := metadata.Commit(); err != nil {
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
	var id int64
	if err := tx.QueryRowContext(ctx, "SELECT customer_id FROM customer WHERE customer_id=1 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		report, err := Backfill(ctx, pool.DB, box, 100, true)
		if err == nil && (!report.Complete || report.Migrated != 0) {
			err = errors.New("backfill overwrote the online protected contact")
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatal("backfill bypassed the customer lock", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := Store(ctx, tx, box, id, Strings("13800000003", "online replacement")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	_, record, err := scanRecord(pool.QueryRowContext(ctx, "SELECT "+recordColumns+" FROM customer WHERE customer_id=1"))
	values, openErr := record.Open(box, 1)
	if err != nil || openErr != nil || values.Phone == nil || *values.Phone != "13800000003" || values.Address == nil || *values.Address != "online replacement" {
		t.Fatal("backfill lost the online update", err, openErr)
	}
}
