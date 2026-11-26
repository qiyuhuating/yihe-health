package notify

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

// Test delivery eligibility, not the spelling of a former plaintext SQL regexp.
func TestFamilyPhoneRecipientsLegacyAndProtectedMySQL(t *testing.T) {
	pool := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32))))
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO family(family_id,family_phone) VALUES(1,'13800000001'),(2,'invalid'),(3,NULL),(4,NULL)")
	exec("INSERT INTO oldperson(old_id,name,family_id) VALUES(1,'one',1),(2,'two',2),(3,'three',3),(4,'four',4)")
	exec("INSERT INTO device(device_id,device_no,secret) VALUES(1,'PHONE-NOTIFY',?)", strings.Repeat("x", 48))
	for id := 1; id <= 4; id++ {
		exec("INSERT INTO care_incident(id,old_id,device_id) VALUES(?,?,1)", id, id)
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := userphone.StoreFamily(ctx, tx, box, 3, "13800000003"); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 3; id++ {
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := QueueCareSMS(ctx, tx, box, int64(id), int64(id)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := CareInbox(ctx, tx, box, int64(id), int64(id)); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_notification_outbox").Scan(&count); err != nil || count != 2 {
		t.Fatal("valid legacy/protected recipients lost or invalid legacy phone queued", err)
	}
	for id, phone := range map[int]string{1: "13800000001", 3: "13800000003"} {
		var stored string
		if err := pool.QueryRowContext(ctx, "SELECT recipient FROM care_notification_outbox WHERE incident_id=?", id).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if opened, err := OpenNotificationRecipient(box, stored); err != nil || opened != phone || stored == phone {
			t.Fatal("notification recipient was not correctly re-encrypted")
		}
	}
	var inboxRecipient string
	if err := pool.QueryRowContext(ctx, "SELECT recipient FROM message_outbox WHERE topic='care' AND family_id=3 AND channel='in_app'").Scan(&inboxRecipient); err != nil {
		t.Fatal(err)
	}
	if opened, err := box.Open(inboxRecipient, "notification-recipient"); err != nil || opened != "13800000003" {
		t.Fatal("protected family lost its encrypted in-app recipient")
	}
	wrongRow, err := userphone.ProtectFamily(box, 3, "13800000004")
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE family SET family_phone_cipher=?,family_phone_lookup=?,family_phone_protected=true WHERE family_id=4", wrongRow.Ciphertext, wrongRow.Lookup)
	for _, row := range []struct {
		id  int64
		box *securestore.Box
	}{{4, box}, {3, nil}} {
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = QueueCareSMS(ctx, tx, row.box, row.id, row.id)
		inboxErr := CareInbox(ctx, tx, row.box, row.id, row.id)
		tx.Rollback()
		if err == nil || inboxErr == nil {
			t.Fatal("missing key or wrong family scope was treated as no recipient/success")
		}
	}
}
