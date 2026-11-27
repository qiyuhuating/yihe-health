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
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/nats-io/nats.go/jetstream"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryrepo"
	"yihe-care/internal/vitalrepo"
)

func TestRustVitalConsumer(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Skip("YIHE_NEXT_GATEWAY_BINARY not set")
	}
	db := dbtest.MySQL(t).DB
	installSinkSchema(t, db)
	sinkExec(t, db, "CREATE TABLE rust_vital_receipt(device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,payload_digest BINARY(64) NOT NULL,measurement_id BIGINT NOT NULL,revision BIGINT NOT NULL,PRIMARY KEY(device_id,binding_epoch,request_id),UNIQUE KEY revision_key(device_id,binding_epoch,revision),UNIQUE KEY measurement_key(measurement_id)) ENGINE=InnoDB")
	sinkExec(t, db, "CREATE TABLE rust_vital_rejection(device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,payload_digest BINARY(64) NOT NULL,reason VARCHAR(32) NOT NULL,PRIMARY KEY(device_id,binding_epoch,request_id,payload_digest)) ENGINE=InnoDB")
	sinkExec(t, db, "INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic-rust'),(2,'synthetic-go')")
	sinkExec(t, db, "INSERT INTO device(device_id,device_no,secret,old_id,status) VALUES(1,'VITAL001','fixture',1,1),(2,'VITAL002','fixture',2,1)")
	now := time.Now().UTC().Truncate(time.Second)
	sinkExec(t, db, "UPDATE device SET bind_time=? WHERE device_id IN (1,2)", now.Add(-time.Hour))
	sinkExec(t, db, "INSERT INTO watch_info(device_id,hrf,hrl,bpf,bpl) VALUES(1,100,60,145,95),(2,100,60,145,95)")
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	box, err := securestore.New(key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	server, nc := broker(t, t.TempDir())
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: "YIHE_vitals", Subjects: []string{"yihe.next.vitals"}, Storage: jetstream.FileStorage, Retention: jetstream.WorkQueuePolicy, Discard: jetstream.DiscardNew, Replicas: 1, MaxBytes: 1 << 20, MaxMsgs: 20, MaxMsgSize: 256 * 1024, Duplicates: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stream.CreateConsumer(ctx, jetstream.ConsumerConfig{Durable: "commit", FilterSubject: "yihe.next.vitals", AckPolicy: jetstream.AckExplicitPolicy, DeliverPolicy: jetstream.DeliverAllPolicy, AckWait: 30 * time.Second, MaxDeliver: -1, MaxAckPending: 64})
	if err != nil {
		t.Fatal(err)
	}
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
	run := func(mode, arg string, want int) []byte {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, mode, arg)
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
			t.Fatalf("vital command %s exit %d want %d: %s", mode, actual, want, output)
		}
		return output
	}
	publish := func(id string, hr, bp *int, at time.Time, scope string) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"version": 1, "device_id": 1, "resident_id": 1, "binding_epoch": 1, "auth_generation": 1, "request_id": id, "received_at": now, "observed_at": at, "hr": hr, "bp": bp})
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := box.Seal(string(raw), scope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = js.Publish(ctx, "yihe.next.vitals", []byte(sealed)); err != nil {
			t.Fatal(err)
		}
	}
	const scope = "yihe-next-vitals-event-v1"
	hr, bp := 120, 150
	at := now.Add(-time.Minute)
	// Inject receipt failure after real encrypted measurement and alert state.
	sinkExec(t, db, "CREATE TRIGGER rust_fail_vital BEFORE INSERT ON rust_vital_receipt FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	publish("first", &hr, &bp, at, scope)
	run("--consume-vital-one-test", addr, 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM ratepre", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM device_health_alert_state", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_receipt", 0)
	sinkExec(t, db, "DROP TRIGGER rust_fail_vital")
	run("--consume-vital-one-test", addr, 0)
	// Compare actual Go repository with the native writer on equivalent device.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = telemetryrepo.InsertVitals(ctx, tx, 2, 2, 1, at, &hr, &bp, "1", "1", box); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, device := range []int64{1, 2} {
		var id int64
		var owner, dev *int64
		var rate, pre string
		var stored vitalrepo.StoredMeasurements
		if err = db.QueryRowContext(ctx, "SELECT id,old_id,device_id,rate_state,pre_state,hr,bp,measurement_cipher FROM ratepre WHERE device_id=?", device).Scan(&id, &owner, &dev, &rate, &pre, &stored.HeartRate, &stored.Pressure, &stored.Cipher); err != nil {
			t.Fatal(err)
		}
		opened, err := stored.Open(box, id, owner, dev)
		if err != nil || stored.HeartRate != nil || stored.Pressure != nil || opened.HeartRate == nil || *opened.HeartRate != 120 || opened.Pressure == nil || *opened.Pressure != 150 || rate != "1" || pre != "1" {
			t.Fatalf("protected repository mismatch: %v", err)
		}
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM device_health_alert_state WHERE heart_rate_alert AND blood_pressure_alert", 2)
	publish("first", &hr, &bp, at, scope)
	run("--consume-vital-one-test", addr, 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM ratepre WHERE device_id=1", 1)
	normal := 80
	publish("first", &normal, &bp, at, scope)
	run("--consume-vital-one-test", addr, 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_rejection WHERE reason='request_id_conflict'", 1)
	publish("second", &normal, nil, at.Add(time.Second), scope)
	run("--consume-vital-one-test", addr, 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM device_health_alert_state WHERE device_id=1 AND heart_rate_alert AND blood_pressure_alert", 1)
	stats := func(want map[string]float64) {
		t.Helper()
		var got map[string]float64
		if err = json.Unmarshal(run("--vital-stats-test", "1", 0), &got); err != nil {
			t.Fatal(err)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("stats %s=%v want %v", k, got[k], v)
			}
		}
	}
	stats(map[string]float64{"revision": 2, "count": 2, "min": 80, "max": 120, "mean": 100, "appends": 1, "rebuilds": 1})
	old := 60
	publish("historical", &old, nil, at.Add(-time.Second), scope)
	run("--consume-vital-one-test", addr, 0)
	publish("pressure-only", nil, &bp, at.Add(2*time.Second), scope)
	run("--consume-vital-one-test", addr, 0)
	stats(map[string]float64{"revision": 4, "count": 3, "min": 60, "max": 120, "mean": 260.0 / 3, "appends": 1, "rebuilds": 3})
	// A persistent projection starts from committed revision 4 and only
	// consumes the new revision after native broker consumption.
	projection := exec.CommandContext(ctx, binary, "--vital-stats-worker-test", "1")
	projection.Env = env
	projectionOut, err := projection.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	projection.Stderr = os.Stderr
	if err = projection.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = projection.Process.Kill(); _ = projection.Wait() }()
	projectionReader := bufio.NewReader(projectionOut)
	projectionCheck := func(revision, count, appends, rebuilds float64) {
		t.Helper()
		line, err := projectionReader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]float64
		if err = json.Unmarshal(line, &result); err != nil {
			t.Fatal(err)
		}
		if result["revision"] != revision || result["count"] != count || result["appends"] != appends || result["rebuilds"] != rebuilds {
			t.Fatalf("persistent projection mismatch: %s", line)
		}
	}
	projectionCheck(4, 3, 1, 3)
	// Independent canonical Go encoder + actual native admission and publish.
	sinkExec(t, db, "CREATE TABLE rust_device_key(device_id BIGINT PRIMARY KEY,generation BIGINT NOT NULL,public_key BINARY(32) NOT NULL) ENGINE=InnoDB")
	sinkExec(t, db, "CREATE TABLE rust_vital_outbox(id BIGINT AUTO_INCREMENT PRIMARY KEY,device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,fingerprint BINARY(64) NOT NULL,encoded MEDIUMBLOB NULL,published_at DATETIME(6) NULL,UNIQUE KEY admission(device_id,binding_epoch,request_id),KEY pending_device(device_id,published_at),KEY pending_dispatch(published_at,id),CHECK ((encoded IS NULL)=(published_at IS NOT NULL))) ENGINE=InnoDB")
	seed := bytes.Repeat([]byte{13}, 32)
	private := ed25519.NewKeyFromSeed(seed)
	sinkExec(t, db, "INSERT INTO rust_device_key SELECT device_id,generation,? FROM broker_device_generation WHERE device_id=1", []byte(private.Public().(ed25519.PublicKey)))
	gateway := exec.CommandContext(ctx, binary, "--native-outbox-test", "127.0.0.1:0")
	gateway.Env = env
	stdout, err := gateway.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	gateway.Stderr = os.Stderr
	if err = gateway.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gateway.Process.Kill(); _ = gateway.Wait() }()
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	fieldVar := func(dst []byte, tag, n uint64) []byte {
		return wirebin.AppendUvarint(wirebin.AppendUvarint(dst, tag<<3), n)
	}
	fieldBytes := func(dst []byte, tag uint64, value []byte) []byte {
		return append(wirebin.AppendUvarint(wirebin.AppendUvarint(dst, tag<<3|2), uint64(len(value))), value...)
	}
	packet := func(id, nonce, domain string, hr uint64) []byte {
		p := fieldVar(nil, 1, 1)
		p = fieldBytes(p, 2, []byte("VITAL001"))
		p = fieldVar(p, 3, 1)
		p = fieldBytes(p, 4, []byte(id))
		p = fieldBytes(p, 5, []byte(nonce))
		p = fieldVar(p, 6, uint64(time.Now().Unix()))
		p = fieldVar(p, 7, uint64(at.Add(3*time.Second).Unix()))
		p = fieldVar(p, 8, hr)
		return fieldBytes(fieldBytes(nil, 1, p), 2, ed25519.Sign(private, append([]byte(domain+"\x00"), p...)))
	}
	post := func(raw []byte, path string, want int) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+strings.TrimSpace(address)+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-protobuf")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("vital admission %d want %d", resp.StatusCode, want)
		}
	}
	const wireDomain = "yihe-next-vital-wire-v1"
	const vitalPath = "/api/v2/device/vitals"
	signed := packet("signed-vital", "vital-sign-nonce-001", wireDomain, 90)
	bad := append([]byte(nil), signed...)
	bad[len(bad)-1] ^= 1
	post(bad, vitalPath, 401)
	post(signed, "/api/v2/device/step", 401)
	post(packet("signed-vital", "vital-sign-nonce-001", "yihe-next-step-v1", 90), vitalPath, 401)
	sinkExec(t, db, "CREATE TRIGGER rust_fail_vital_admit BEFORE INSERT ON rust_vital_outbox FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	post(signed, vitalPath, 500)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_outbox", 0)
	digest := sha256.Sum256([]byte("VITAL001\x00vital-sign-nonce-001"))
	var nonces int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_request_nonces WHERE nonce_digest=?", digest[:]).Scan(&nonces); err != nil || nonces != 0 {
		t.Fatalf("admission rollback nonce: %v %d", err, nonces)
	}
	sinkExec(t, db, "DROP TRIGGER rust_fail_vital_admit")
	post(signed, vitalPath, 202)
	post(signed, vitalPath, 409)
	var original []byte
	if err = db.QueryRowContext(ctx, "SELECT encoded FROM rust_vital_outbox").Scan(&original); err != nil {
		t.Fatal(err)
	}
	post(packet("signed-vital", "vital-sign-nonce-002", wireDomain, 90), vitalPath, 202)
	var retry []byte
	if err = db.QueryRowContext(ctx, "SELECT encoded FROM rust_vital_outbox").Scan(&retry); err != nil || !bytes.Equal(original, retry) {
		t.Fatal("vital retry changed envelope")
	}
	post(packet("signed-vital", "vital-sign-nonce-003", wireDomain, 91), vitalPath, 409)
	sinkCount(t, db, "SELECT COUNT(*) FROM ratepre WHERE device_id=1", 4)
	run("--dispatch-vital-crash-test", addr, 77)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_outbox WHERE published_at IS NULL", 1)
	run("--consume-vital-one-test", addr, 0)
	run("--dispatch-vital-one-test", addr, 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_outbox WHERE published_at IS NOT NULL AND encoded IS NULL", 1)
	stats(map[string]float64{"revision": 5, "count": 4, "min": 60, "max": 120, "mean": 87.5, "appends": 2, "rebuilds": 3})
	projectionCheck(5, 4, 2, 3)
	t.Log("signed vital admission, separate signature domain, nonce rollback/replay, immutable outbox and real PubAck crash recovery passed")
	// Revocation is terminal; no revision or protected row is created.
	publish("revoked", &hr, nil, at, scope)
	sinkExec(t, db, "UPDATE device SET secret='rotated-fixture' WHERE device_id=1")
	run("--consume-vital-one-test", addr, 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_rejection WHERE reason='authorization_generation'", 1)
	post(packet("revoked-key", "vital-revoked-nonce01", wireDomain, 90), vitalPath, 401)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_receipt", 5)
	sinkCount(t, db, "SELECT COUNT(*) FROM ratepre WHERE device_id=1", 5)
	// A correctly encrypted event from the wrong domain cannot be consumed.
	publish("wrong-domain", &hr, nil, at, "yihe-next-telemetry-event-v1")
	run("--consume-vital-one-test", addr, 1)
	info, err := stream.Info(ctx)
	if err != nil || info.State.Msgs != 1 {
		t.Fatalf("wrong domain not retained: %v", err)
	}
	// The current binding's projection must never expose the previous
	// resident's samples, even when the durable receipts remain.
	sinkExec(t, db, "UPDATE device SET old_id=2 WHERE device_id=1")
	stats(map[string]float64{"revision": 0, "count": 0, "appends": 0, "rebuilds": 0})
	projectionCheck(0, 0, 0, 0)
	t.Log("Rust protected vitals, Go repository/decryption parity, rollback, idempotency, committed revision incremental append/rebuild, revocation, binding isolation and domain isolation passed")
}
