package lane

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/securestore"
	"yihe-care/internal/telemetryworkflow"
)

// These DDL statements are installed exclusively in dbtest's disposable
// migrated schema. No default production migration or deployment is changed.
func installSinkSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, ddl := range []string{
		"CREATE TABLE broker_device_generation (device_id BIGINT PRIMARY KEY, generation BIGINT NOT NULL) ENGINE=InnoDB",
		"CREATE TABLE broker_telemetry_receipt (device_id BIGINT NOT NULL, binding_epoch BIGINT NOT NULL, request_id VARBINARY(64) NOT NULL, payload_digest BINARY(64) NOT NULL, PRIMARY KEY(device_id,binding_epoch,request_id)) ENGINE=InnoDB",
		"CREATE TABLE broker_telemetry_rejection (device_id BIGINT NOT NULL, binding_epoch BIGINT NOT NULL, request_id VARBINARY(64) NOT NULL, payload_digest BINARY(64) NOT NULL, reason VARCHAR(32) NOT NULL, PRIMARY KEY(device_id,binding_epoch,request_id,payload_digest)) ENGINE=InnoDB",
		"CREATE TRIGGER broker_device_insert AFTER INSERT ON device FOR EACH ROW INSERT INTO broker_device_generation VALUES(NEW.device_id,1) ON DUPLICATE KEY UPDATE generation=generation+1",
		`CREATE TRIGGER broker_device_change AFTER UPDATE ON device FOR EACH ROW
			BEGIN
				IF NOT (NEW.old_id <=> OLD.old_id) OR NOT (NEW.status <=> OLD.status) OR NOT (BINARY NEW.secret <=> BINARY OLD.secret) THEN
					UPDATE broker_device_generation SET generation=generation+1 WHERE device_id=NEW.device_id;
				END IF;
			END`,
		"CREATE TRIGGER broker_device_delete AFTER DELETE ON device FOR EACH ROW UPDATE broker_device_generation SET generation=generation+1 WHERE device_id=OLD.device_id",
		"CREATE TABLE broker_test_writes (id INT PRIMARY KEY, n INT NOT NULL) ENGINE=InnoDB",
		"INSERT INTO broker_test_writes VALUES(1,0)",
		"CREATE TRIGGER broker_test_day_insert AFTER INSERT ON watch_day_info FOR EACH ROW UPDATE broker_test_writes SET n=n+1 WHERE id=1",
		"CREATE TRIGGER broker_test_day_update AFTER UPDATE ON watch_day_info FOR EACH ROW UPDATE broker_test_writes SET n=n+1 WHERE id=1",
	} {
		sinkExec(t, db, ddl)
	}
}

func sinkExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func sinkCount(t *testing.T, db *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(t.Context(), query).Scan(&got); err != nil || got != want {
		t.Fatalf("%s: got %d, want %d, err=%v", query, got, want, err)
	}
}

func TestMySQLSinkAtomicCommitAndRevocation(t *testing.T) {
	pool := dbtest.MySQL(t)
	db := pool.DB
	installSinkSchema(t, db)
	sinkExec(t, db, "INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic-1'),(2,'synthetic-2')")
	sinkExec(t, db, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'SYNTHETIC',?,1)", "fixture-only-credential")
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	sink := MySQLSink{DB: db, Box: box}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, nc := broker(t, t.TempDir())
	l := openLane(t, ctx, nc, Telemetry, 20)
	step := 1200
	now := time.Now().UTC().Truncate(time.Microsecond)
	event := TelemetryEvent{DeviceID: 1, ResidentID: 1, BindingEpoch: 1, AuthGeneration: 1, RequestID: "atomic-1",
		Payload: telemetryworkflow.Payload{ReceivedAt: now, ReportedAt: now, Step: &step}}
	seal := func(event TelemetryEvent) []byte {
		t.Helper()
		sealed, err := SealTelemetry(box, event)
		if err != nil {
			t.Fatal(err)
		}
		return sealed
	}
	publish := func(event TelemetryEvent) {
		t.Helper()
		if _, err := l.Publish(ctx, seal(event)); err != nil {
			t.Fatal(err)
		}
	}
	consume := func() {
		t.Helper()
		if err := l.CommitOne(ctx, sink.Commit); err != nil {
			t.Fatal(err)
		}
	}

	// Receipt failure must roll back actual daily metrics AND heartbeat.
	sinkExec(t, db, "CREATE TRIGGER broker_fail_receipt BEFORE INSERT ON broker_telemetry_receipt FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected receipt failure'")
	publish(event)
	if err := l.CommitOne(ctx, sink.Commit); err == nil {
		t.Fatal("receipt failure incorrectly acknowledged")
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM watch_day_info", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 0)
	sinkCount(t, db, "SELECT COUNT(*) FROM device WHERE last_time IS NOT NULL OR last_online_at IS NOT NULL", 0)
	sinkExec(t, db, "DROP TRIGGER broker_fail_receipt")
	consume()
	sinkCount(t, db, "SELECT step FROM watch_day_info", 1200)
	sinkCount(t, db, "SELECT n FROM broker_test_writes WHERE id=1", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM device WHERE last_time IS NOT NULL AND last_online_at IS NOT NULL", 1)

	// Re-sealing creates different ciphertext, bypassing broker byte dedup;
	// the durable SQL receipt still prevents a second business mutation.
	publish(event)
	consume()
	sinkCount(t, db, "SELECT n FROM broker_test_writes WHERE id=1", 1)

	// A different authenticated payload cannot reuse the same idempotency key.
	other := 2400
	conflict := event
	conflict.Payload.Step = &other
	publish(conflict)
	consume()
	sinkCount(t, db, "SELECT step FROM watch_day_info", 1200)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='request_id_conflict'", 1)

	// An event accepted before revocation may not apply after reactivation.
	event.RequestID = "revoked-1"
	publish(event)
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	sinkCount(t, db, "SELECT health_epoch FROM device WHERE device_id=1", 1)
	sinkCount(t, db, "SELECT generation FROM broker_device_generation WHERE device_id=1", 3)
	consume()
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='authorization_generation'", 1)

	// A queued event authorized before credential rotation also becomes stale.
	event.AuthGeneration = 3
	event.RequestID = "rotated-1"
	publish(event)
	sinkExec(t, db, "UPDATE device SET secret='fixture-rotated-credential' WHERE device_id=1")
	consume()
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='authorization_generation'", 2)

	// Rebinding invalidates both the old resident and binding epoch.
	event.AuthGeneration = 4
	event.RequestID = "rebound-1"
	publish(event)
	sinkExec(t, db, "UPDATE device SET old_id=2 WHERE device_id=1")
	consume()
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='binding_or_status'", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 1)
	sinkCount(t, db, "SELECT n FROM broker_test_writes WHERE id=1", 1)

	// A correctly authenticated new-generation event can still advance data.
	event.ResidentID = 2
	event.BindingEpoch = 2
	event.AuthGeneration = 5
	event.RequestID = "new-binding-1"
	event.Payload.Step = &other
	publish(event)
	consume()
	sinkCount(t, db, "SELECT step FROM watch_day_info WHERE old_id=2", 2400)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 2)

	tampered := seal(event)
	tampered[len(tampered)/2] ^= 1
	if err := sink.Commit(ctx, tampered); err == nil {
		t.Fatal("tampered event accepted")
	}
	info, err := l.consumer.Info(ctx)
	if err != nil || info.NumPending != 0 || info.NumAckPending != 0 {
		t.Fatalf("unsettled consumer after durable terminal outcomes: %v, %v", info, err)
	}
}
