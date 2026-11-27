package lane

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	wirebin "encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Second)
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
	// Real independent SOS ingress and publisher; ordinary admission
	// permits are exhausted by incomplete requests on a separate listener.
	sinkExec(t, db, "CREATE TABLE rust_device_key(device_id BIGINT PRIMARY KEY,generation BIGINT NOT NULL,public_key BINARY(32) NOT NULL) ENGINE=InnoDB")
	sinkExec(t, db, "CREATE TABLE rust_sos_outbox(id BIGINT AUTO_INCREMENT PRIMARY KEY,device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,fingerprint BINARY(64) NOT NULL,encoded MEDIUMBLOB NULL,published_at DATETIME(6) NULL,UNIQUE KEY admission(device_id,request_id),KEY pending_device(device_id,published_at),KEY pending_dispatch(published_at,id),CHECK ((encoded IS NULL)=(published_at IS NOT NULL))) ENGINE=InnoDB")
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{17}, 32))
	sinkExec(t, db, "INSERT INTO rust_device_key SELECT device_id,generation,? FROM broker_device_generation WHERE device_id=1", []byte(private.Public().(ed25519.PublicKey)))
	listen := func(mode string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, mode, "127.0.0.1:0")
		cmd.Env = env
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		cmd.Stderr = os.Stderr
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		address, err := bufio.NewReader(stdout).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(address)
	}
	ordinary := listen("--native-outbox-test")
	emergency := listen("--native-sos-test")
	client := &http.Client{Timeout: 8 * time.Second}
	post := func(address, path string, raw []byte) int {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	packet := func(id, nonce, domain string) []byte {
		fieldVar := func(dst []byte, tag, n uint64) []byte {
			return wirebin.AppendUvarint(wirebin.AppendUvarint(dst, tag<<3), n)
		}
		fieldBytes := func(dst []byte, tag uint64, value []byte) []byte {
			return append(wirebin.AppendUvarint(wirebin.AppendUvarint(dst, tag<<3|2), uint64(len(value))), value...)
		}
		p := fieldVar(nil, 1, 1)
		p = fieldBytes(p, 2, []byte("SOS001"))
		p = fieldVar(p, 3, 1)
		p = fieldBytes(p, 4, []byte(id))
		p = fieldBytes(p, 5, []byte(nonce))
		p = fieldVar(p, 6, uint64(time.Now().Unix()))
		return fieldBytes(fieldBytes(nil, 1, p), 2, ed25519.Sign(private, append([]byte(domain+"\x00"), p...)))
	}
	const path = "/api/v2/device/sos"
	const domain = "yihe-next-sos-wire-v1"
	signed := packet("sos-signed", "sos-signed-nonce-001", domain)
	expect := func(raw []byte, want int) {
		t.Helper()
		if got := post(emergency, path, raw); got != want {
			t.Fatalf("SOS signed admission %d want %d", got, want)
		}
	}
	if post(ordinary, path, signed) != 404 || post(emergency, "/api/v2/device/step", signed) != 404 {
		t.Fatal("listeners not isolated")
	}
	bad := append([]byte(nil), signed...)
	bad[len(bad)-1] ^= 1
	expect(bad, 401)
	expect(packet("sos-signed", "sos-signed-nonce-001", "yihe-next-step-v1"), 401)
	sinkExec(t, db, "CREATE TRIGGER rust_fail_sos_admit BEFORE INSERT ON rust_sos_outbox FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	expect(signed, 500)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_sos_outbox", 0)
	digest := sha256.Sum256([]byte("SOS001\x00sos-signed-nonce-001"))
	var nonces int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_request_nonces WHERE nonce_digest=?", digest[:]).Scan(&nonces); err != nil || nonces != 0 {
		t.Fatalf("nonce rollback: %d %v", nonces, err)
	}
	sinkExec(t, db, "DROP TRIGGER rust_fail_sos_admit")
	var blocked []net.Conn
	for range 8 {
		c, err := net.DialTimeout("tcp", ordinary, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		blocked = append(blocked, c)
		defer c.Close()
		if _, err = fmt.Fprintf(c, "POST /api/v2/device/step HTTP/1.1\r\nHost: %s\r\nContent-Type: application/x-protobuf\r\nContent-Length: 64\r\n\r\n", ordinary); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for post(ordinary, "/api/v2/device/step", nil) != 503 {
		if time.Now().After(deadline) {
			t.Fatal("ordinary admission not saturated")
		}
		time.Sleep(10 * time.Millisecond)
	}
	expect(signed, 202)
	for _, c := range blocked {
		_ = c.Close()
	}
	expect(signed, 409)
	var original, retry []byte
	if err = db.QueryRowContext(ctx, "SELECT encoded FROM rust_sos_outbox").Scan(&original); err != nil {
		t.Fatal(err)
	}
	expect(packet("sos-signed", "sos-signed-nonce-002", domain), 202)
	if err = db.QueryRowContext(ctx, "SELECT encoded FROM rust_sos_outbox").Scan(&retry); err != nil || !bytes.Equal(original, retry) {
		t.Fatal("SOS retry changed envelope")
	}
	counts(1)
	run("--dispatch-sos-crash-test", 77)
	run("--consume-sos-one-test", 0)
	run("--dispatch-sos-one-test", 0)
	counts(2)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_sos_outbox WHERE published_at IS NOT NULL AND encoded IS NULL", 1)
	t.Log("signed SOS admission succeeds while ordinary permits exhausted; atomic nonce, immutable outbox, PubAck crash and commit verified")
	publish("sos-revoked", now)
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	run("--consume-sos-one-test", 0)
	counts(2)
	expect(packet("sos-revoked-key", "sos-revoked-nonce-01", domain), 401)
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
