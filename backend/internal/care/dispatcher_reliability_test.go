package care

import (
	"bytes"
	"context"
	"encoding/base64"
	"testing"

	"yihe-care/internal/securestore"
)

// A wrong key or a corrupt encrypted recipient must remain a failed delivery.
// Scheduling a voice job for the empty decrypted value is not an escalation.
func TestUndecryptableCareRecipientNeverSchedulesVoiceFallback(t *testing.T) {
	pool := testCareDB(t)
	ctx := context.Background()
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.ExecContext(ctx, `INSERT INTO care_notification_outbox
		(id, incident_id, channel, recipient, status, attempts, retry_at)
		VALUES(1,1,'sms','sne1:invalid-envelope','failed',4,CURRENT_TIMESTAMP(6)-INTERVAL 1 MINUTE)`)
	if err != nil {
		t.Fatal(err)
	}
	d := &dispatcher{db: pool, box: box}
	processed, err := d.dispatchNext(ctx)
	if err != nil || !processed {
		t.Fatalf("failed encrypted recipient was not claimed: processed=%v err=%v", processed, err)
	}
	var jobs, voice, attempts int
	var status string
	if err := pool.QueryRowContext(ctx, `SELECT COUNT(*),
		SUM(channel='voice') FROM care_notification_outbox`).Scan(&jobs, &voice); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRowContext(ctx, "SELECT status,attempts FROM care_notification_outbox WHERE id=1").Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 || voice != 0 || status != "failed" || attempts != 5 {
		t.Fatalf("failed recipient decryption created unusable voice delivery: jobs=%d voice=%d status=%s attempts=%d", jobs, voice, status, attempts)
	}
}
