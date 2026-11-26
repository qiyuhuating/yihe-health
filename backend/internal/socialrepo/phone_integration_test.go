package socialrepo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
)

const socialTestPhone = "13800000001"

func socialTestBox(t *testing.T, encryption, lookup string) *securestore.Box {
	t.Helper()
	key := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(strings.Repeat(value, 32))) }
	box, err := securestore.NewKeyring("A", map[string]string{"A": key(encryption)}, key(lookup))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func socialExec(t *testing.T, pool *db.Database, query string, args ...any) {
	t.Helper()
	if _, err := pool.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func seedProtectedSocialComment(t *testing.T, pool *db.Database, box *securestore.Box) {
	t.Helper()
	tx, err := pool.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	created, err := InsertComment(context.Background(), tx, box, NewComment{NoticeID: 1, Content: "synthetic", AuthorPhone: socialTestPhone, CreatedAt: time.Now()})
	if err != nil || !created {
		t.Fatal("cannot seed protected comment", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestSocialPhoneBackfillMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	box := socialTestBox(t, "A", "B")
	socialExec(t, pool, "INSERT INTO said(said_id,text_id,said_phone,nice_count) VALUES(1,1,?,1),(2,1,NULL,0)", socialTestPhone)
	socialExec(t, pool, "INSERT INTO nice_count(said_id,phone) VALUES(1,?)", socialTestPhone)
	read := func() []Comment {
		t.Helper()
		items, err := ListComments(ctx, pool, box, socialTestPhone, 1)
		if err != nil || len(items) != 2 {
			t.Fatal("read failed", err)
		}
		return items
	}
	before, _ := json.Marshal(read())
	report, err := BackfillPhones(ctx, pool.DB, box, 1, false)
	if err != nil || report.Before != 2 || report.Migrated != 0 || report.Complete {
		t.Fatal("scan mutated or lost inventory", report, err)
	}
	var binding int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM social_phone_key_state").Scan(&binding); err != nil || binding != 0 {
		t.Fatal("scan created metadata", err)
	}
	report, err = BackfillPhones(ctx, pool.DB, box, 1, true)
	if !errors.Is(err, ErrPhoneBackfillIncomplete) || report.Migrated != 1 || report.Remaining == nil || *report.Remaining != 1 || report.Complete {
		t.Fatal("bounded migration hid incomplete result", report, err)
	}
	partial, _ := json.Marshal(read())
	if string(before) != string(partial) {
		t.Fatal("partially migrated comments and legacy votes changed the public response")
	}
	report, err = BackfillPhones(ctx, pool.DB, box, 1, true)
	if err != nil || report.Migrated != 1 || !report.Complete {
		t.Fatal("migration did not resume", report, err)
	}
	after, _ := json.Marshal(read())
	if string(before) != string(after) {
		t.Fatal("backfill changed the public comment model")
	}
	report, err = BackfillPhones(ctx, pool.DB, box, 100, true)
	if err != nil || !report.Complete || report.Migrated != 0 {
		t.Fatal("rerun was not idempotent", report, err)
	}
	var raw int
	if err := pool.QueryRowContext(ctx, "SELECT (SELECT COUNT(said_phone) FROM said)+(SELECT COUNT(phone) FROM nice_count)").Scan(&raw); err != nil || raw != 0 {
		t.Fatal("legacy phone copies remain", err)
	}
	if _, err := ToggleLike(ctx, pool, box, 1, socialTestPhone); err != nil {
		t.Fatal("migrated vote identity could not be toggled", err)
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT nice_count FROM said WHERE said_id=1").Scan(&count); err != nil || count != 0 {
		t.Fatal("migrated identity changed vote counting", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := BackfillPhones(canceled, pool.DB, box, 1, true); err == nil {
		t.Fatal("canceled migration reported success")
	}
}

func TestSocialPhonePreflightMySQL(t *testing.T) {
	for _, scenario := range []string{"duplicate", "mixed duplicate", "invalid", "orphan", "aggregate", "corrupt ciphertext"} {
		t.Run(scenario, func(t *testing.T) {
			pool := dbtest.MySQL(t)
			box := socialTestBox(t, "A", "B")
			socialExec(t, pool, "INSERT INTO said(said_id,text_id,said_phone,nice_count) VALUES(1,1,?,1)", socialTestPhone)
			socialExec(t, pool, "INSERT INTO nice_count(said_id,phone) VALUES(1,?)", socialTestPhone)
			switch scenario {
			case "duplicate":
				socialExec(t, pool, "INSERT INTO nice_count(said_id,phone) VALUES(1,?)", socialTestPhone)
				socialExec(t, pool, "UPDATE said SET nice_count=2 WHERE said_id=1")
			case "mixed duplicate":
				seedProtectedSocialComment(t, pool, box)
				socialExec(t, pool, "INSERT INTO nice_count(said_id,phone_lookup,phone_protected) VALUES(1,?,true)", box.Index("social-like-phone", socialTestPhone))
				socialExec(t, pool, "UPDATE said SET nice_count=2 WHERE said_id=1")
			case "invalid":
				socialExec(t, pool, "UPDATE nice_count SET phone='bad historical value'")
			case "orphan":
				socialExec(t, pool, "UPDATE nice_count SET said_id=999")
			case "aggregate":
				socialExec(t, pool, "UPDATE said SET nice_count=0 WHERE said_id=1")
			case "corrupt ciphertext":
				seedProtectedSocialComment(t, pool, box)
				socialExec(t, pool, "UPDATE said SET said_phone_cipher='sne1:v2:A:***' WHERE said_id=2")
			}
			report, err := BackfillPhones(context.Background(), pool.DB, box, 100, true)
			if err == nil || report.Migrated != 0 || report.Complete {
				t.Fatal("preflight allowed mutation of an invalid inventory", report, err)
			}
			var raw string
			if err := pool.QueryRowContext(context.Background(), "SELECT said_phone FROM said WHERE said_id=1").Scan(&raw); err != nil || raw != socialTestPhone {
				t.Fatal("earlier valid row changed before preflight finished", err)
			}
		})
	}
}

func TestSocialPhoneFailClosedMySQL(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"missing key", "wrong lookup key", "wrong AEAD key", "missing binding", "wrong row", "plaintext ciphertext", "damaged ciphertext"} {
		t.Run(scenario, func(t *testing.T) {
			pool := dbtest.MySQL(t)
			box := socialTestBox(t, "A", "B")
			seedProtectedSocialComment(t, pool, box)
			seedProtectedSocialComment(t, pool, box)
			switch scenario {
			case "missing key":
				box = nil
			case "wrong lookup key":
				box = socialTestBox(t, "A", "C")
			case "wrong AEAD key":
				box = socialTestBox(t, "C", "B")
			case "missing binding":
				socialExec(t, pool, "DELETE FROM social_phone_key_state")
			case "wrong row":
				socialExec(t, pool, "UPDATE said a JOIN said b ON b.said_id=1 SET a.said_phone_cipher=b.said_phone_cipher WHERE a.said_id=2")
			case "plaintext ciphertext":
				socialExec(t, pool, "UPDATE said SET said_phone_cipher=? WHERE said_id=2", socialTestPhone)
			case "damaged ciphertext":
				socialExec(t, pool, "UPDATE said SET said_phone_cipher='sne1:v2:A:***' WHERE said_id=2")
			}
			if items, err := ListComments(ctx, pool, box, socialTestPhone, 1); err == nil || len(items) != 0 {
				t.Fatal("invalid protected identity returned a partial or successful response")
			}
			if scenario == "missing key" || scenario == "wrong lookup key" || scenario == "missing binding" {
				if _, err := ToggleLike(ctx, pool, box, 1, socialTestPhone); err == nil {
					t.Fatal("unverified key wrote a new vote")
				}
				var count int
				if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM nice_count").Scan(&count); err != nil || count != 0 {
					t.Fatal("failed vote left a durable row", err)
				}
			}
		})
	}
}

func TestSocialPhoneConcurrentVotesMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	box := socialTestBox(t, "A", "B")
	seedProtectedSocialComment(t, pool, box)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var group sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		group.Go(func() {
			_, err := ToggleLike(ctx, pool, box, 1, socialTestPhone)
			errors <- err
		})
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal("concurrent vote did not serialize", err)
		}
	}
	var votes, aggregate int
	if err := pool.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM nice_count WHERE said_id=1),nice_count FROM said WHERE said_id=1").Scan(&votes, &aggregate); err != nil || votes != 0 || aggregate != 0 {
		t.Fatal("concurrent toggles duplicated an identity or changed aggregate", err)
	}
	// A legacy vote still matches the same protected-mode viewer.
	socialExec(t, pool, "INSERT INTO nice_count(said_id,phone) VALUES(1,?)", socialTestPhone)
	socialExec(t, pool, "UPDATE said SET nice_count=1 WHERE said_id=1")
	if _, err := ToggleLike(ctx, pool, box, 1, socialTestPhone); err != nil {
		t.Fatal("legacy vote was not matched by protected writer", err)
	}
	// DB constraints reject mixed metadata independently of application code.
	for _, statement := range []string{
		"UPDATE said SET said_phone='13800000001' WHERE said_id=1",
		"INSERT INTO nice_count(said_id,phone_protected) VALUES(1,true)",
		"INSERT INTO nice_count(phone_lookup,phone_protected) VALUES(REPEAT('a',64),true)",
		"INSERT INTO nice_count(said_id,phone,phone_lookup,phone_protected) VALUES(1,'13800000001',REPEAT('a',64),true)",
	} {
		if _, err := pool.ExecContext(ctx, statement); err == nil {
			t.Fatal("database accepted inconsistent protected metadata")
		}
	}
	if _, err := ToggleLike(ctx, pool, box, 1, socialTestPhone); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if deleted, err := DeleteComment(ctx, tx, 1); err != nil || !deleted {
		t.Fatal("comment deletion failed", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM nice_count").Scan(&votes); err != nil || votes != 0 {
		t.Fatal("comment deletion retained a protected vote identity", err)
	}
}

func TestSocialPhoneBackfillLocksMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	box := socialTestBox(t, "A", "B")
	socialExec(t, pool, "INSERT INTO said(said_id,text_id,nice_count) VALUES(1,1,1)")
	socialExec(t, pool, "INSERT INTO nice_count(said_id,phone) VALUES(1,?)", socialTestPhone)
	legacy, err := ListComments(ctx, pool, nil, socialTestPhone, 1)
	if err != nil || len(legacy) != 1 || legacy[0].Good == nil || *legacy[0].Good != "1" {
		t.Fatal("keyless legacy read changed", err)
	}
	lock, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var parent int64
	if err := lock.QueryRowContext(ctx, "SELECT said_id FROM said WHERE said_id=1 FOR UPDATE").Scan(&parent); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if changed, _, err := backfillSocialPhoneOne(blocked, pool.DB, box); err == nil || changed {
		t.Fatal("backfill bypassed the parent vote lock or reported a canceled write")
	}
	if err := lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	var raw string
	var bound int
	if err := pool.QueryRowContext(ctx, "SELECT phone,(SELECT COUNT(*) FROM social_phone_key_state) FROM nice_count WHERE said_id=1").Scan(&raw, &bound); err != nil || raw != socialTestPhone || bound != 0 {
		t.Fatal("canceled row transaction retained a binding or changed the vote", err)
	}
	// Race a migration row transaction with cancellation of the same legacy vote.
	// Either lock winner is valid; a stale scan must never resurrect a removed vote.
	var group sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, 2)
	group.Go(func() {
		<-start
		_, _, err := backfillSocialPhoneOne(ctx, pool.DB, box)
		results <- err
	})
	group.Go(func() {
		<-start
		_, err := ToggleLike(ctx, pool, box, 1, socialTestPhone)
		results <- err
	})
	close(start)
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("row migration and vote did not serialize", err)
		}
	}
	var votes, aggregate int
	if err := pool.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM nice_count WHERE said_id=1),nice_count FROM said WHERE said_id=1").Scan(&votes, &aggregate); err != nil || votes != 0 || aggregate != 0 {
		t.Fatal("backfill overwrote a concurrent vote cancellation", err)
	}
	if report, err := BackfillPhones(ctx, pool.DB, box, 1, true); err != nil || !report.Complete || report.Migrated != 0 {
		t.Fatal("canceled backfill could not safely resume", err)
	}
}

func TestSocialCommentViewerWithoutPhoneMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx := context.Background()
	socialExec(t, pool, "INSERT INTO said(said_id,text_id,said_phone) VALUES(1,1,?)", socialTestPhone)
	items, err := ListComments(ctx, pool, nil, "", 1)
	if err != nil || len(items) != 1 || items[0].Good == nil || *items[0].Good != "0" {
		t.Fatal("authorized legacy viewer without a phone could not read comments", err)
	}
	box := socialTestBox(t, "A", "B")
	seedProtectedSocialComment(t, pool, box)
	items, err = ListComments(ctx, pool, box, "", 1)
	if err != nil || len(items) != 2 {
		t.Fatal("authorized viewer without a phone could not read protected comments", err)
	}
	for _, item := range items {
		if item.SaidPhone == nil || *item.SaidPhone != "138****0001" || item.Good == nil || *item.Good != "0" {
			t.Fatal("phone-less viewer changed author masking or acquired another identity's vote")
		}
	}
	if _, err := ToggleLike(ctx, pool, box, 1, ""); err == nil {
		t.Fatal("read compatibility permitted a vote with an empty identity")
	}
	var votes int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM nice_count").Scan(&votes); err != nil || votes != 0 {
		t.Fatal("invalid viewer left a durable vote", err)
	}
}
