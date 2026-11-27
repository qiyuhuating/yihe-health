package lane

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	wirebin "encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/nats-io/nats.go/jetstream"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/devicecredential"
	"yihe-care/internal/securestore"
)

func TestRustOutboxProducer(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Skip("YIHE_NEXT_GATEWAY_BINARY not set")
	}
	pool := dbtest.MySQL(t)
	db := pool.DB
	installSinkSchema(t, db)
	sinkExec(t, db, `CREATE TABLE rust_step_outbox(
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		device_id BIGINT NOT NULL,binding_epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,
		fingerprint BINARY(32) NOT NULL,encoded MEDIUMBLOB NULL,published_at DATETIME(6) NULL,
		UNIQUE KEY admission(device_id,binding_epoch,request_id),
		KEY pending_device(device_id,published_at),
		KEY pending_dispatch(published_at,id),
		CHECK ((encoded IS NULL) = (published_at IS NOT NULL))) ENGINE=InnoDB`)
	secret := strings.Repeat("s", 32)
	hash, err := devicecredential.Hash(secret)
	if err != nil {
		t.Fatal(err)
	}
	sinkExec(t, db, "INSERT INTO oldperson(old_id,name) VALUES(1,'synthetic')")
	sinkExec(t, db, "INSERT INTO device(device_id,device_no,secret,old_id,status) VALUES(1,'WATCH001',?,1,1)", hash)
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	box, err := securestore.New(key)
	if err != nil {
		t.Fatal(err)
	}
	sink := MySQLSink{DB: db, Box: box}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
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
	cmd := exec.CommandContext(ctx, binary, "--native-outbox-test", "127.0.0.1:0")
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
	url := "http://" + strings.TrimSpace(address) + "/api/v1/device/report"
	client := &http.Client{Timeout: 10 * time.Second}
	now := time.Now().UTC()
	body := func(id string, step int) []byte {
		data, err := json.Marshal(map[string]any{"deviceNo": "WATCH001", "secret": secret, "bindingVersion": 1, "requestId": id, "step": step, "beTime": now.Add(-time.Minute).Format("2006-01-02 15:04:05")})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	post := func(raw []byte, nonce string, want int, code string) {
		t.Helper()
		ts := fmt.Sprint(time.Now().Unix())
		digest := sha256.Sum256(raw)
		mac := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(mac, "WATCH001:%s:%s:%s", ts, nonce, hex.EncodeToString(digest[:]))
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-Device-Signature", hex.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-Device-Timestamp", ts)
		r.Header.Set("X-Device-Nonce", nonce)
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want || (code != "" && result["code"] != code) || (want == 202 && result["state"] != "admitted") {
			t.Fatalf("admission status %d code %v state %v, want %d %s", resp.StatusCode, result["code"], result["state"], want, code)
		}
	}
	dispatch := func(mode, addr string, want int) {
		t.Helper()
		c := exec.CommandContext(ctx, binary, mode, addr)
		c.Env = env
		output, err := c.CombinedOutput()
		actual := 0
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				actual = e.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		if actual != want {
			t.Fatalf("dispatch %s: exit %d want %d: %s", mode, actual, want, output)
		}
	}
	pending := func(want int) {
		sinkCount(t, db, "SELECT COUNT(*) FROM rust_step_outbox WHERE encoded IS NOT NULL", want)
	}
	raw := body("outbox-1", 1200)
	sinkExec(t, db, "CREATE TRIGGER rust_fail_admission BEFORE INSERT ON rust_step_outbox FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	post(raw, "outbox-insert-fail-01", 500, "INTERNAL_ERROR")
	sinkCount(t, db, "SELECT COUNT(*) FROM device_request_nonces", 0)
	pending(0)
	sinkExec(t, db, "DROP TRIGGER rust_fail_admission")
	post(raw, "outbox-insert-fail-01", 202, "")
	pending(1)
	sinkCount(t, db, "SELECT COUNT(*) FROM watch_day_info", 0)
	var encoded []byte
	if err = db.QueryRowContext(ctx, "SELECT encoded FROM rust_step_outbox WHERE request_id='outbox-1'").Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	plain, err := box.Open(string(encoded), brokerEventScope)
	if err != nil {
		t.Fatal("Rust AES-GCM envelope incompatible with Go")
	}
	var event TelemetryEvent
	if err = json.Unmarshal([]byte(plain), &event); err != nil {
		t.Fatal(err)
	}
	if event.DeviceID != 1 || event.ResidentID != 1 || event.BindingEpoch != 1 || event.AuthGeneration != 1 || event.RequestID != "outbox-1" || event.Payload.Step == nil || *event.Payload.Step != 1200 {
		t.Fatal("event fields differ")
	}
	if bytes.Contains(encoded, []byte(secret)) || strings.Contains(plain, secret) {
		t.Fatal("credential retained in event")
	}
	post(raw, "outbox-insert-fail-01", 409, "NONCE_REPLAY")
	post(raw, "outbox-fresh-retry-01", 202, "")
	var retry []byte
	if err = db.QueryRowContext(ctx, "SELECT encoded FROM rust_step_outbox WHERE request_id='outbox-1'").Scan(&retry); err != nil || !bytes.Equal(encoded, retry) {
		t.Fatal("retry changed immutable envelope")
	}
	post(body("outbox-1", 1300), "outbox-conflicting-01", 409, "REQUEST_ID_CONFLICT")
	sinkCount(t, db, "SELECT COUNT(*) FROM device_request_nonces", 2)

	// No broker is listening: committed admission must remain available.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unavailable := listener.Addr().String()
	listener.Close()
	dispatch("--dispatch-one-test", unavailable, 1)
	pending(1)
	server, nc := broker(t, t.TempDir())
	addr := strings.TrimPrefix(server.ClientURL(), "nats://")
	l := openLane(t, ctx, nc, Telemetry, 20)

	// Real child process exits after PubAck, before the SQL update.
	dispatch("--dispatch-crash-test", addr, 77)
	pending(1)
	if err = l.CommitOne(ctx, func(ctx context.Context, data []byte) error {
		if !bytes.Equal(data, encoded) {
			return fmt.Errorf("published bytes differ from outbox")
		}
		return sink.Commit(ctx, data)
	}); err != nil {
		t.Fatal(err)
	}
	sinkCount(t, db, "SELECT step FROM watch_day_info", 1200)
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 1)
	dispatch("--dispatch-one-test", addr, 0)
	pending(0)
	stream, err := l.js.Stream(ctx, "YIHE_telemetry")
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil || info.State.Msgs != 0 {
		t.Fatalf("broker dedup after restart: %v", err)
	}
	sinkCount(t, db, "SELECT COUNT(*) FROM rust_step_outbox WHERE published_at IS NOT NULL", 1)
	post(raw, "outbox-after-pub-001", 202, "")
	pending(0)
	// Bypass finite transport dedup to exercise the durable business receipt.
	if _, err = l.js.Publish(ctx, l.subject, encoded, jetstream.WithMsgID("simulated-after-dedup-window")); err != nil {
		t.Fatal(err)
	}
	if err = l.CommitOne(ctx, sink.Commit); err != nil {
		t.Fatal(err)
	}
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 1)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_receipt", 1)

	// A committed admission revoked before delivery is terminally rejected.
	raw2 := body("outbox-2", 2400)
	post(raw2, "outbox-before-revoke-01", 202, "")
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	dispatch("--dispatch-one-test", addr, 0)
	if err = l.CommitOne(ctx, sink.Commit); err != nil {
		t.Fatal(err)
	}
	pending(0)
	sinkCount(t, db, "SELECT COUNT(*) FROM broker_telemetry_rejection WHERE reason='authorization_generation'", 1)
	sinkCount(t, db, "SELECT step FROM watch_day_info", 1200)
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 1)
	dispatch("--dispatch-one-test", addr, 0)
	info, err = stream.Info(ctx)
	if err != nil || info.State.Msgs != 0 {
		t.Fatalf("unconsumed broker messages: %v", err)
	}
	// The simulator sends an actual signed binary request. The independent
	// Go encoder also checks the wire tags and Ed25519 domain, not just a
	// shared Rust implementation.
	simulator := os.Getenv("YIHE_NEXT_WATCH_BINARY")
	if simulator == "" {
		t.Fatal("YIHE_NEXT_WATCH_BINARY required for binary protocol test")
	}
	sinkExec(t, db, "CREATE TABLE rust_device_key(device_id BIGINT PRIMARY KEY,generation BIGINT NOT NULL,public_key BINARY(32) NOT NULL) ENGINE=InnoDB")
	seed := bytes.Repeat([]byte{11}, 32)
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	sinkExec(t, db, "INSERT INTO rust_device_key SELECT device_id,generation,? FROM broker_device_generation WHERE device_id=1", []byte(public))
	observed := now.Add(-time.Minute).Unix()
	watch := exec.CommandContext(ctx, simulator, "--send", strings.TrimSpace(address), "WATCH001", "1", "binary-sim-1", "binary-sim-nonce-001", strconv.FormatInt(observed, 10), "3200")
	watch.Env = append(os.Environ(), "WATCH_NEXT_SEED_HEX="+hex.EncodeToString(seed))
	if output, err := watch.CombinedOutput(); err != nil || !bytes.HasPrefix(output, []byte("202 ")) {
		t.Fatalf("simulator: %v %s", err, output)
	}
	post(body("binary-sim-1", 3200), "binary-sim-nonce-001", 409, "NONCE_REPLAY")
	// Encode canonical protobuf with the Go standard library, independently of prost.
	fieldVar := func(dst []byte, tag, n uint64) []byte {
		return wirebin.AppendUvarint(wirebin.AppendUvarint(dst, tag<<3), n)
	}
	fieldBytes := func(dst []byte, tag uint64, value []byte) []byte {
		return append(wirebin.AppendUvarint(wirebin.AppendUvarint(dst, tag<<3|2), uint64(len(value))), value...)
	}
	packet := func(id, nonce string, n uint64, key ed25519.PrivateKey) []byte {
		p := fieldVar(nil, 1, 1)
		p = fieldBytes(p, 2, []byte("WATCH001"))
		p = fieldVar(p, 3, 1)
		p = fieldBytes(p, 4, []byte(id))
		p = fieldBytes(p, 5, []byte(nonce))
		p = fieldVar(p, 6, uint64(time.Now().Unix()))
		p = fieldVar(p, 7, uint64(observed))
		p = fieldVar(p, 8, n)
		signature := ed25519.Sign(key, append([]byte("yihe-next-step-v1\x00"), p...))
		return fieldBytes(fieldBytes(nil, 1, p), 2, signature)
	}
	binaryPost := func(data []byte, want int, code string) {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+strings.TrimSpace(address)+"/api/v2/device/step", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/x-protobuf")
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want || (code != "" && result["code"] != code) {
			t.Fatalf("binary status %d code %v want %d/%s", resp.StatusCode, result["code"], want, code)
		}
	}
	p := packet("binary-go-1", "binary-go-nonce-0001", 3300, private)
	bad := append([]byte(nil), p...)
	bad[len(bad)-1] ^= 1
	binaryPost(bad, 401, "DEVICE_SIGNATURE_INVALID")
	binaryPost(append(append([]byte(nil), p...), 0x18, 1), 400, "INVALID_REQUEST")
	binaryPost(p, 202, "")
	binaryPost(p, 409, "NONCE_REPLAY")
	pending(2)
	// A single worker publishes both records, then handles a later arrival
	// without rebuilding its NATS connection or SQL pool.
	before, err := server.Varz(nil)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	worker := exec.CommandContext(workerCtx, binary, "--dispatch-worker-test", addr)
	worker.Env = env
	worker.Stderr = os.Stderr
	if err = worker.Start(); err != nil {
		stopWorker()
		t.Fatal(err)
	}
	defer func() {
		stopWorker()
		if worker.ProcessState == nil {
			_ = worker.Wait()
		}
	}()
	waitPending := func(want int) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			var count int
			if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM rust_step_outbox WHERE published_at IS NULL").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("worker pending %d want %d", count, want)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	waitPending(0)
	for range 2 {
		if err = l.CommitOne(ctx, sink.Commit); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	binaryPost(packet("binary-go-2", "binary-go-nonce-0002", 3400, private), 202, "")
	waitPending(0)
	if err = l.CommitOne(ctx, sink.Commit); err != nil {
		t.Fatal(err)
	}
	after, err := server.Varz(nil)
	if err != nil || after.TotalConnections-before.TotalConnections != 1 {
		t.Fatalf("worker did not reuse broker connection: %v", err)
	}
	sinkCount(t, db, "SELECT n FROM broker_test_writes", 4)
	sinkCount(t, db, "SELECT step FROM watch_day_info", 3400)
	stopWorker()
	_ = worker.Wait()
	// Revoking the generation invalidates the enrolled key immediately.
	sinkExec(t, db, "UPDATE device SET status=0 WHERE device_id=1")
	sinkExec(t, db, "UPDATE device SET status=1 WHERE device_id=1")
	binaryPost(packet("binary-revoked", "binary-revoked-0001", 3500, private), 401, "DEVICE_AUTH_FAILED")
	pending(0)
	t.Log("binary simulator, independent Go wire encoder, cross-protocol replay, revoked key and one NATS connection across three publications passed")
	// Exercise the actual per-device admission bound. Pre-fill pending rows
	// using the same valid envelope; these remain disposable fixture data.
	for i := range 64 {
		sinkExec(t, db, "INSERT INTO rust_step_outbox(device_id,binding_epoch,request_id,fingerprint,encoded) VALUES(1,1,?,UNHEX(SHA2(?,256)),?)", fmt.Sprintf("capacity-%d", i), fmt.Sprintf("capacity-%d", i), encoded)
	}
	post(body("over-capacity", 3000), "outbox-capacity-0001", 503, "SERVICE_UNAVAILABLE")
	pending(64)
	nonceDigest := sha256.Sum256([]byte("WATCH001\x00outbox-capacity-0001"))
	var count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_request_nonces WHERE nonce_digest=?", nonceDigest[:]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("capacity rejection consumed nonce: %d %v", count, err)
	}
	t.Log("Rust admission, Go envelope authentication, immutable retries, unavailable broker, process exit after real PubAck, durable consumer dedup and revoked generation passed")
}
