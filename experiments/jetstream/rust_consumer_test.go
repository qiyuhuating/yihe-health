package lane

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"yihe-care/internal/telemetryworkflow"
)

func TestRustNativeConsumer(t *testing.T) {
	executable := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if executable == "" {
		t.Skip("YIHE_NEXT_GATEWAY_BINARY not set")
	}
	db := dbtest.MySQL(t).DB
	installSinkSchema(t, db)
	sinkExec(t, db, "INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')")
	sinkExec(t, db, "INSERT INTO device(device_id,device_no,secret,old_id,status) VALUES(1,'WATCH001','fixture',1,1)")
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	box, err := securestore.New(key)
	if err != nil {
		t.Fatal(err)
	}
	sink := MySQLSink{DB: db, Box: box}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Second)
	defer cancel()
	server, nc := broker(t, t.TempDir())
	l := openLane(t, ctx, nc, Telemetry, 20)
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
		cmd := exec.CommandContext(ctx, executable, mode, addr)
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
			t.Fatalf("consumer exit %d want %d: %s", actual, want, output)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	step := 1200
	e := TelemetryEvent{DeviceID: 1, ResidentID: 1, BindingEpoch: 1, AuthGeneration: 1, RequestID: "rust-consume-1", Payload: telemetryworkflow.Payload{ReceivedAt: now, ReportedAt: now, Step: &step}}
	seal := func(event TelemetryEvent) []byte {
		t.Helper()
		raw, err := SealTelemetry(box, event)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	publish := func(raw []byte, id string) {
		t.Helper()
		if _, err := l.js.Publish(ctx, l.subject, raw, jetstream.WithMsgID(id)); err != nil {
			t.Fatal(err)
		}
	}
	raw := seal(e)
	publish(raw, "first")
	sinkExec(t, db, "CREATE TRIGGER rust_fail_receipt BEFORE INSERT ON broker_telemetry_receipt FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	run("--consume-one-test", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM watch_day_info", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM device WHERE last_online_at IS NOT NULL", 0)
	sinkExec(t, db, "DROP TRIGGER rust_fail_receipt")
	time.Sleep(1100 * time.Millisecond) // Wait for the explicit delayed NAK.
	// Actual process exits after COMMIT and before ACK.
	run("--consume-crash-test", 78)
	sinkCount(t, db, "SELECT step FROM watch_day_info", 1200)
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 1)
	info, err := l.consumer.Info(ctx)
	if err != nil || info.NumAckPending != 1 {
		t.Fatalf("crash did not leave pending ACK: %v", err)
	}
	// Use the actual AckWait to observe redelivery, not a synthetic replay.
	deadline := time.Now().Add(36 * time.Second)
	for {
		err = l.CommitOne(ctx, sink.Commit)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("redelivery after commit: %v", err)
		}
	}
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection", 0)
	// Go -> Rust replay (new envelope, identical semantic digest).
	e.RequestID = "rust-consume-2"
	step = 1300
	e.Payload.ReportedAt = now.Add(time.Second)
	publish(seal(e), "go-first")
	if err = l.CommitOne(ctx, sink.Commit); err != nil {
		t.Fatal(err)
	}
	publish(seal(e), "rust-replay")
	run("--consume-one-test", 0)
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 2)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection", 0)
	step = 1400
	publish(seal(e), "conflict")
	run("--consume-one-test", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='request_id_conflict'", 1)
	sinkCount(t, db, "SELECT step FROM watch_day_info", 1300)
	e.RequestID = "rust-revoked"
	publish(seal(e), "revoked")
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	run("--consume-one-test", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='authorization_generation'", 1)
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 2)
	// Invalid envelope must remain unacknowledged, never mutate the database.
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)/2] ^= 1
	publish(tampered, "tamper")
	run("--consume-one-test", 1)
	stream, err := l.js.Stream(ctx, "YIHE_telemetry")
	if err != nil {
		t.Fatal(err)
	}
	state, err := stream.Info(ctx)
	if err != nil || state.State.Msgs != 1 {
		t.Fatalf("invalid event not retained: %v", err)
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 2)
	t.Log("Rust rollback, actual post-COMMIT crash/redelivery, shared Go receipt in both directions, conflict, revoked generation and tamper passed")
}
