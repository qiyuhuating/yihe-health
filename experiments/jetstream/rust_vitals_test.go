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
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
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
	// Revocation is terminal; no revision or protected row is created.
	publish("revoked", &hr, nil, at, scope)
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	run("--consume-vital-one-test", addr, 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_rejection WHERE reason='authorization_generation'", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_vital_receipt", 4)
	sinkCount(t, db, "SELECT COUNT(*) FROM ratepre WHERE device_id=1", 4)
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
	t.Log("Rust protected vitals, Go repository/decryption parity, rollback, idempotency, committed revision incremental append/rebuild, revocation, binding isolation and domain isolation passed")
}
