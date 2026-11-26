package familyaddress

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

func TestFamilyAddressBackfillMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	key := func(letter string) *securestore.Box {
		t.Helper()
		box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat(letter, 32))))
		if err != nil {
			t.Fatal(err)
		}
		return box
	}
	box := key("A")
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	longAddress := strings.Repeat("址", 198) + "  "
	exec("INSERT INTO family(family_id,family_address) VALUES(1,?),(2,''),(3,NULL),(4,?)", longAddress, longAddress)
	var pendingCount int
	assertPending := func(want int) {
		t.Helper()
		if err := pool.QueryRowContext(ctx, countPending).Scan(&pendingCount); err != nil || pendingCount != want {
			t.Fatalf("pending addresses = %d, want %d: %v", pendingCount, want, err)
		}
	}
	report, err := Backfill(ctx, pool.DB, box, 1, false)
	if err != nil || report.Before != 3 || report.Migrated != 0 || report.Complete {
		t.Fatal("scan did not report untouched historical addresses", err)
	}
	assertPending(3)
	report, err = Backfill(ctx, pool.DB, box, 1, true)
	if !errors.Is(err, ErrBackfillIncomplete) || report.Migrated != 1 || report.Remaining == nil || *report.Remaining != 2 {
		t.Fatal("bounded apply failed to report remaining addresses", err)
	}
	report, err = Backfill(ctx, pool.DB, key("B"), 100, true)
	if err == nil || report.Migrated != 0 {
		t.Fatal("wrong reader rewrote remaining plaintext", err)
	}
	assertPending(2)
	report, err = Backfill(ctx, pool.DB, box, 100, true)
	if err != nil || report.Migrated != 2 || !report.Complete {
		t.Fatal("apply did not resume remaining historical addresses", err)
	}
	var firstCipher string
	for i, want := range []string{longAddress, "", "", longAddress} {
		id := int64(i + 1)
		var record Record
		if err := pool.QueryRowContext(ctx, "SELECT "+Columns+" FROM family WHERE family_id=?", id).
			Scan(&record.Legacy, &record.Cipher, &record.Protected); err != nil {
			t.Fatal(err)
		}
		plain, err := record.Open(box, id)
		if id == 3 {
			if err != nil || plain != nil || record.Protected || record.Cipher != nil {
				t.Fatal("legacy NULL changed", err)
			}
			continue
		}
		if err != nil || plain == nil || *plain != want || record.Legacy != nil || !record.Protected {
			t.Fatalf("historical address semantics changed for synthetic row %d: %v", id, err)
		}
		if id == 1 {
			firstCipher = *record.Cipher
		}
		if id == 4 && *record.Cipher == firstCipher {
			t.Fatal("duplicate addresses reused ciphertext")
		}
	}
	report, err = Backfill(ctx, pool.DB, box, 100, true)
	if err != nil || report.Migrated != 0 || !report.Complete {
		t.Fatal("backfill was not idempotent", err)
	}
	exec("UPDATE family SET family_address_cipher='sne1:corrupt' WHERE family_id=4")
	exec("UPDATE family SET family_address='pending' WHERE family_id=3")
	if report, err = Backfill(ctx, pool.DB, box, 100, true); err == nil || report.Migrated != 0 {
		t.Fatal("bad ciphertext failed to stop all pending writes", err)
	}
	assertPending(1)
}
