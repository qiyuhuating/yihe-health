//go:build yihe_next_gateway

package handlers

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/devicecredential"
)

func TestNextGatewayNativeStep(t *testing.T) {
	t.Setenv("YIHE_REQUIRE_SIGNED_DEVICE", "1")
	directDB, nativeDB := testDatabase(t), testDatabase(t)
	seedDevice(t, directDB)
	seedDevice(t, nativeDB)
	ctx := t.Context()
	var schema string
	if err := nativeDB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
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
	cmd := exec.CommandContext(ctx, nextGatewayBinary(t), "--native-step-test", "127.0.0.1:0")
	cmd.Env = append(os.Environ(), "YIHE_NEXT_DB_HOST="+host, "YIHE_NEXT_DB_PORT="+port, "YIHE_NEXT_DB_SCHEMA="+schema, "YIHE_NEXT_DB_USER="+cfg.User, "YIHE_NEXT_DB_PASSWORD="+cfg.Passwd)
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
	direct := testHandler(t, directDB)
	shared := testHandler(t, nativeDB)
	client := &http.Client{Timeout: 15 * time.Second}
	body := func(step int, epoch int, secret, at string) []byte {
		raw, err := json.MarshalIndent(map[string]any{"deviceNo": "WATCH001", "secret": secret, "bindingVersion": epoch, "beTime": at, "step": step}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	sign := func(raw []byte, secret, nonce string, at time.Time) http.Header {
		ts := fmt.Sprint(at.Unix())
		digest := sha256.Sum256(raw)
		mac := hmac.New(sha256.New, []byte(secret))
		fmt.Fprintf(mac, "WATCH001:%s:%s:%s", ts, nonce, hex.EncodeToString(digest[:]))
		h := make(http.Header)
		h.Set("X-Device-Signature", hex.EncodeToString(mac.Sum(nil)))
		h.Set("X-Device-Timestamp", ts)
		h.Set("X-Device-Nonce", nonce)
		h.Set("Content-Type", "application/json")
		return h
	}
	type result struct {
		status int
		fields map[string]any
		err    error
	}
	send := func(handler http.HandlerFunc, raw []byte, headers http.Header) result {
		if handler != nil {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/device/report", bytes.NewReader(raw))
			r.Header = headers.Clone()
			handler(w, r)
			var fields map[string]any
			err := json.Unmarshal(w.Body.Bytes(), &fields)
			return result{w.Code, fields, err}
		}
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
		if err != nil {
			return result{err: err}
		}
		r.Header = headers.Clone()
		w, err := client.Do(r)
		if err != nil {
			return result{err: err}
		}
		defer w.Body.Close()
		var fields map[string]any
		err = json.NewDecoder(w.Body).Decode(&fields)
		return result{w.StatusCode, fields, err}
	}
	check := func(r result, status int, code string) {
		t.Helper()
		if r.err != nil || r.status != status || (code != "" && r.fields["code"] != code) {
			// Do not include request payloads or credentials in failures.
			t.Fatalf("response: status=%d code=%v err=%v want=%d/%s", r.status, r.fields["code"], r.err, status, code)
		}
	}
	cases := 0
	compare := func(raw []byte, headers http.Header, status int, code string) {
		t.Helper()
		a, b := send(direct.deviceReport, raw, headers), send(nil, raw, headers)
		check(a, status, code)
		check(b, status, code)
		if a.fields["returnCode"] != b.fields["returnCode"] {
			t.Fatal("returnCode differs")
		}
		if status == 200 {
			delete(a.fields, "serverTime")
			delete(b.fields, "serverTime")
			if !reflect.DeepEqual(a.fields, b.fields) {
				t.Fatal("success envelope differs")
			}
		}
		cases++
	}
	now := time.Now().UTC()
	// Same day, ordered and out-of-order reports.
	at := now.Format("2006-01-02") + " 00:02:00"
	earlier := now.Format("2006-01-02") + " 00:01:00"
	if now.Hour() == 0 && now.Minute() < 3 {
		at = now.AddDate(0, 0, -1).Format("2006-01-02") + " 00:02:00"
		earlier = now.AddDate(0, 0, -1).Format("2006-01-02") + " 00:01:00"
	}
	raw := body(100, 1, testSecret, at)
	compare(raw, http.Header{}, 401, "DEVICE_SIGNATURE_INVALID")
	h := sign(raw, testSecret, "native-expired-0001", now.Add(-20*time.Minute))
	compare(raw, h, 401, "DEVICE_SIGNATURE_INVALID")
	h = sign(raw, testSecret, "native-tampered-001", now)
	h.Set("X-Device-Signature", strings.Repeat("0", 64))
	compare(raw, h, 401, "DEVICE_SIGNATURE_INVALID")
	wrong := strings.Repeat("q", 32)
	invalid := body(100, 1, wrong, at)
	compare(invalid, sign(invalid, wrong, "native-wrong-key-01", now), 401, "DEVICE_AUTH_FAILED")
	h = sign(raw, testSecret, "native-initial-0001", now)
	compare(raw, h, 200, "")
	compare(raw, h, 409, "NONCE_REPLAY")
	old := body(20, 1, testSecret, earlier)
	compare(old, sign(old, testSecret, "native-older-00001", now), 200, "")
	var a, b int64
	query := "SELECT step FROM watch_day_info WHERE device_id=1"
	if err = directDB.QueryRowContext(ctx, query).Scan(&a); err != nil {
		t.Fatal(err)
	}
	if err = nativeDB.QueryRowContext(ctx, query).Scan(&b); err != nil {
		t.Fatal(err)
	}
	if a != 100 || b != 100 {
		t.Fatalf("out-of-order update: %d/%d", a, b)
	}

	// Old Go and new Rust must share the same nonce namespace.
	cross := sign(raw, testSecret, "native-cross-go-001", now)
	check(send(shared.deviceReport, raw, cross), 200, "")
	check(send(nil, raw, cross), 409, "NONCE_REPLAY")
	cross = sign(raw, testSecret, "native-cross-rs-001", now)
	check(send(nil, raw, cross), 200, "")
	check(send(shared.deviceReport, raw, cross), 409, "NONCE_REPLAY")
	// Concurrent requests through separate connections cannot both commit.
	concurrent := sign(raw, testSecret, "native-concurrent-01", now)
	var wg sync.WaitGroup
	results := make(chan result, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- send(nil, raw, concurrent) }()
	}
	wg.Wait()
	close(results)
	var successes, replays int
	for r := range results {
		if r.status == 200 && r.err == nil {
			successes++
		} else {
			check(r, 409, "NONCE_REPLAY")
			replays++
		}
	}
	if successes != 1 || replays != 1 {
		t.Fatalf("concurrent nonce: %d/%d", successes, replays)
	}

	// Inject actual business-write failure after the nonce INSERT. A retry
	// with the same signature must be accepted after the trigger is removed.
	mustExec(t, nativeDB, "CREATE TRIGGER next_fail_step BEFORE UPDATE ON watch_day_info FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	retry := body(150, 1, testSecret, at)
	retryHeaders := sign(retry, testSecret, "native-rollback-001", now)
	check(send(nil, retry, retryHeaders), 500, "INTERNAL_ERROR")
	digest := sha256.Sum256([]byte("WATCH001\x00native-rollback-001"))
	var count int
	if err = nativeDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_request_nonces WHERE device_id=1 AND nonce_digest=?", digest[:]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("nonce rollback: %d %v", count, err)
	}
	mustExec(t, nativeDB, "DROP TRIGGER next_fail_step")
	check(send(nil, retry, retryHeaders), 200, "")
	if err = nativeDB.QueryRowContext(ctx, query).Scan(&b); err != nil || b != 150 {
		t.Fatalf("rollback retry step: %d %v", b, err)
	}

	// Force the native transaction to wait after its unlocked authentication.
	// Observe the actual MySQL lock wait before committing revocation.
	tx, err := nativeDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var residentID int64
	if err = tx.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE").Scan(&residentID); err != nil {
		t.Fatal(err)
	}
	raceHeaders := sign(raw, testSecret, "native-lock-revoke-01", now)
	waiting := make(chan result, 1)
	go func() { waiting <- send(nil, raw, raceHeaders) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waits int
		err = nativeDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM performance_schema.data_lock_waits w
			JOIN performance_schema.data_locks l ON l.ENGINE_LOCK_ID=w.REQUESTING_ENGINE_LOCK_ID
			WHERE l.OBJECT_SCHEMA=? AND l.OBJECT_NAME='oldperson'`, schema).Scan(&waits)
		if err != nil {
			t.Fatal(err)
		}
		if waits > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native resident lock wait not observed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE device SET status=0 WHERE device_id=1"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	check(<-waiting, 403, "DEVICE_DISABLED")
	digest = sha256.Sum256([]byte("WATCH001\x00native-lock-revoke-01"))
	if err = nativeDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_request_nonces WHERE device_id=1 AND nonce_digest=?", digest[:]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("revocation nonce: %d %v", count, err)
	}
	mustExec(t, nativeDB, "UPDATE device SET status=1 WHERE device_id=1")

	// Real credential and binding state changes are read from MySQL.
	mustExec(t, directDB, "UPDATE device SET status=0 WHERE device_id=1")
	mustExec(t, nativeDB, "UPDATE device SET status=0 WHERE device_id=1")
	compare(raw, sign(raw, testSecret, "native-disabled-001", now), 403, "DEVICE_DISABLED")
	mustExec(t, directDB, "UPDATE device SET status=1 WHERE device_id=1")
	mustExec(t, nativeDB, "UPDATE device SET status=1 WHERE device_id=1")
	for _, epoch := range []int{2} {
		future := body(100, epoch, testSecret, at)
		compare(future, sign(future, testSecret, "native-future-0001", now), 409, "BINDING_CHANGED")
	}
	rotated := strings.Repeat("r", 32)
	hash, err := devicecredential.Hash(rotated)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, directDB, "UPDATE device SET secret=? WHERE device_id=1", hash)
	mustExec(t, nativeDB, "UPDATE device SET secret=? WHERE device_id=1", hash)
	compare(raw, sign(raw, testSecret, "native-rotated-001", now), 401, "DEVICE_AUTH_FAILED")
	mustExec(t, directDB, "INSERT INTO oldperson(old_id,name) VALUES(2,'synthetic')")
	mustExec(t, nativeDB, "INSERT INTO oldperson(old_id,name) VALUES(2,'synthetic')")
	mustExec(t, directDB, "UPDATE device SET old_id=2 WHERE device_id=1")
	mustExec(t, nativeDB, "UPDATE device SET old_id=2 WHERE device_id=1")
	stale := body(500, 1, rotated, at)
	compare(stale, sign(stale, rotated, "native-stale-00001", now), 409, "EPOCH_EXPIRED")
	current := body(500, 2, rotated, at)
	compare(current, sign(current, rotated, "native-new-epoch-01", now), 200, "")
	for _, q := range []string{"SELECT step FROM watch_day_info WHERE old_id=2 AND binding_epoch=2", "SELECT COUNT(*) FROM watch_day_info_archive WHERE old_id=1 AND binding_epoch=1", "SELECT COUNT(*) FROM device WHERE last_online_at IS NOT NULL AND old_id=2"} {
		if err = directDB.QueryRowContext(ctx, q).Scan(&a); err != nil {
			t.Fatal(err)
		}
		if err = nativeDB.QueryRowContext(ctx, q).Scan(&b); err != nil {
			t.Fatal(err)
		}
		if a != b || a == 0 {
			t.Fatalf("persisted state differs: %s %d/%d", q, a, b)
		}
	}
	// Unsupported medical or idempotency fields must be rejected, never ignored.
	mixed := bytes.Replace(current, []byte("\n}"), []byte(",\n  \"hr\": 75\n}"), 1)
	check(send(nil, mixed, sign(mixed, rotated, "native-mixed-00001", now)), 400, "INVALID_REQUEST")
	t.Logf("%d independent Go/Rust response comparisons, cross-runtime nonce replay, concurrent replay, rollback, observed lock-wait revocation and 4 persisted-state checks passed", cases)
}
