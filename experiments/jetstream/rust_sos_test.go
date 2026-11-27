package lane

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/careworkflow"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/notify"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

func TestRustSOSConsumer(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Skip("YIHE_NEXT_GATEWAY_BINARY not set")
	}
	db := dbtest.MySQL(t).DB
	installSinkSchema(t, db)
	sinkExec(t, db, "CREATE TABLE rust_sos_receipt(device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,payload_digest BINARY(64) NOT NULL,incident_id BIGINT NOT NULL,PRIMARY KEY(device_id,binding_epoch,request_id)) ENGINE=InnoDB")
	sinkExec(t, db, "CREATE TABLE rust_sos_rejection(device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,payload_digest BINARY(64) NOT NULL,reason VARCHAR(32) NOT NULL,PRIMARY KEY(device_id,binding_epoch,request_id,payload_digest)) ENGINE=InnoDB")
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{19}, 32))
	box, err := securestore.New(key)
	if err != nil {
		t.Fatal(err)
	}
	const phone = "13800000001"
	protected, err := userphone.ProtectFamily(box, 1, phone)
	if err != nil {
		t.Fatal(err)
	}
	sinkExec(t, db, "INSERT INTO family(family_id,family_name,family_phone,family_phone_cipher,family_phone_lookup,family_phone_protected) VALUES(1,'synthetic',NULL,?,?,true)", protected.Ciphertext, protected.Lookup)
	sinkExec(t, db, "INSERT INTO oldperson(old_id,name,family_id) VALUES(1,'synthetic',1)")
	sinkExec(t, db, "INSERT INTO device(device_id,device_no,secret,old_id,status) VALUES(1,'SOS001','fixture',1,1)")
	now := time.Now().UTC().Truncate(time.Microsecond)
	sinkExec(t, db, "UPDATE device SET bind_time=? WHERE device_id=1", now.Add(-time.Hour))
	ctx, cancel := context.WithTimeout(t.Context(), 135*time.Second)
	defer cancel()
	server, nc := broker(t, t.TempDir())
	l := openLane(t, ctx, nc, SOS, 20)
	addr := strings.TrimPrefix(server.ClientURL(), "nats://")
	var schema string
	if err = db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		t.Fatal(err)
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	env := append(os.Environ(), "YIHE_NEXT_DB_HOST="+host, "YIHE_NEXT_DB_PORT="+port, "YIHE_NEXT_DB_SCHEMA="+schema, "YIHE_NEXT_DB_USER="+cfg.User, "YIHE_NEXT_DB_PASSWORD="+cfg.Passwd, "YIHE_NEXT_TEST_EVENT_KEY="+key)
	run := func(mode string, want int) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, mode, addr)
		cmd.Env = env
		output, err := cmd.CombinedOutput()
		actual := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				actual = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if actual != want {
			t.Fatalf("SOS consumer %s exit %d want %d: %s", mode, actual, want, output)
		}
	}
	publish := func(id string, at time.Time) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"version": 1, "device_id": 1, "resident_id": 1, "binding_epoch": 1, "auth_generation": 1, "request_id": id, "received_at": at})
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := box.Seal(string(raw), "yihe-next-sos-event-v1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = l.js.Publish(ctx, l.subject, []byte(sealed)); err != nil {
			t.Fatal(err)
		}
	}
	counts := func(want int) {
		t.Helper()
		for _, table := range []string{"alarm_precess", "care_incident", "care_incident_event", "care_notification_outbox", "message_outbox", "rust_sos_receipt"} {
			sinkCount(t, db, "SELECT COUNT(*) FROM "+table, want)
		}
	}
	sinkExec(t, db, "CREATE TRIGGER rust_fail_sos_receipt BEFORE INSERT ON rust_sos_receipt FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	publish("sos-native-1", now)
	run("--consume-sos-one-test", 1)
	counts(0)
	sinkCount(t, db, "SELECT COUNT(*) FROM resident_location_alert_state", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM device WHERE last_online_at IS NOT NULL", 0)
	sinkExec(t, db, "DROP TRIGGER rust_fail_sos_receipt")
	run("--consume-sos-crash-test", 78)
	counts(1)
	// Actual Go retry lookup recognizes the Rust-created incident and
	// notification state. No delivery service runs in this fixture.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := careworkflow.FindSOSRetry(ctx, tx, 1, "sos-native-1")
	if err != nil || prior == nil || prior.Kind != "sos" || prior.ResidentID != 1 || prior.State != "received" || prior.NotificationState != "pending" {
		tx.Rollback()
		t.Fatalf("Go SOS retry mismatch: %v %v", prior, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var recipient, index, inbox, state, meta string
	if err = db.QueryRowContext(ctx, "SELECT recipient,recipient_key FROM care_notification_outbox WHERE incident_id=?", prior.ID).Scan(&recipient, &index); err != nil {
		t.Fatal(err)
	}
	opened, err := notify.OpenNotificationRecipient(box, recipient)
	if err != nil || opened != phone || recipient == phone || index != box.Index("care-notification-recipient", phone) {
		t.Fatal("SMS recipient protection mismatch")
	}
	if err = db.QueryRowContext(ctx, "SELECT recipient,status FROM message_outbox WHERE topic='care' AND channel='in_app'").Scan(&inbox, &state); err != nil {
		t.Fatal(err)
	}
	opened, err = box.Open(inbox, "notification-recipient")
	if err != nil || opened != phone || inbox == phone || state != "delivered" {
		t.Fatal("in-app recipient protection mismatch")
	}
	if err = db.QueryRowContext(ctx, "SELECT position_metadata FROM alarm_precess").Scan(&meta); err != nil {
		t.Fatal(err)
	}
	opened, err = box.Open(meta, "location")
	if err != nil || opened != "{\"version\":1,\"validity\":\"missing\"}" {
		t.Fatal("position metadata mismatch")
	}
	// Re-deliver after the actual 30s broker ACK timeout.
	run("--consume-sos-one-test", 0)
	counts(1)
	publish("sos-native-1", now)
	run("--consume-sos-one-test", 0)
	counts(1)
	publish("sos-native-1", now.Add(time.Second))
	run("--consume-sos-one-test", 0)
	counts(1)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_sos_rejection WHERE reason='request_id_conflict'", 1)
	publish("sos-revoked", now)
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	run("--consume-sos-one-test", 0)
	counts(1)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_sos_rejection WHERE reason='authorization_generation'", 1)
	stream, err := l.js.Stream(ctx, "YIHE_sos")
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil || info.State.Msgs != 0 {
		t.Fatalf("SOS retained after committed ACK: %v", err)
	}
	t.Log("native SOS alarm, incident, event, protected SMS/in-app notification and receipt atomic rollback/commit, Go retry compatibility and actual redelivery passed")
}
