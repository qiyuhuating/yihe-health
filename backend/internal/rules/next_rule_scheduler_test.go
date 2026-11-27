//go:build yihe_next_gateway

package rules

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/devicecredential"
	"yihe-care/internal/devicerepo"
	"yihe-care/internal/deviceworkflow"
	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

func TestNextCoreRuleScheduler(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Fatal("YIHE_NEXT_GATEWAY_BINARY required")
	}
	database, now := seed(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	execSQL := func(q string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := database.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{41}, 32))
	box, err := securestore.New(key)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := userphone.ProtectFamily(box, 1, "13800000001")
	if err != nil {
		t.Fatal(err)
	}
	execSQL("INSERT INTO family(family_id,family_name,family_phone_cipher,family_phone_lookup,family_phone_protected) VALUES(1,'synthetic',?,?,true)", phone.Ciphertext, phone.Lookup)
	execSQL("UPDATE oldperson SET family_id=1 WHERE old_id=1")
	const sosSecret = "synthetic-sos-secret-123456789012"
	secretHash, err := devicecredential.Hash(sosSecret)
	if err != nil {
		t.Fatal(err)
	}
	execSQL("UPDATE device SET secret=?,status=1,battery=5,last_time=? WHERE device_id=1", secretHash, now.Add(-10*time.Minute))
	execSQL("CREATE TABLE rust_rule_receipt(device_id BIGINT NOT NULL,epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,payload_digest BINARY(64) NOT NULL,PRIMARY KEY(device_id,epoch,request_id)) ENGINE=InnoDB")
	p := Default()
	p.Consecutive = 1
	p.BaselineMinimum = 5
	p.Timezone = "UTC"
	p.NightBegin = 0
	p.NightEnd = 86399
	p.HRHigh = 105
	p.BPHigh = 135
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	execSQL("INSERT INTO rule_policy(device_id,revision,payload,updated_by) VALUES(1,7,?,1)", payload)
	device := int64(1)
	sample := func(id, metric, unit string, value float64, at time.Time) health.Sample {
		return health.Sample{RecordID: id, Source: "watch", SourceDevice: "RULE001", Metric: metric, Unit: unit, Value: health.Number(value), Start: at, End: at, Version: 1}
	}
	var history []health.Sample
	for i := 0; i < 5; i++ {
		at := now.Add(-time.Duration(60+i) * time.Minute)
		history = append(history, sample(fmt.Sprintf("hr-base-%d", i), "heart_rate", "count/min", 80, at), sample(fmt.Sprintf("bp-base-%d", i), "blood_pressure_systolic", "mmHg", 120, at))
	}
	heart := sample("hr-current", "heart_rate", "count/min", 150, now.Add(-time.Second))
	blood := sample("bp-current", "blood_pressure_systolic", "mmHg", 190, now.Add(-time.Second))
	steps := sample("steps-current", "steps", "count", 400, now.Add(-time.Minute))
	steps.Semantics = "interval"
	steps.End = now.Add(-time.Second)
	history = append(history, heart, blood, steps)
	save := func(samples []health.Sample) {
		t.Helper()
		b, err := health.PrepareBatch(samples)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = b.SaveProtected(ctx, tx, health.Owner{OldID: 1, DeviceID: &device, Epoch: 1}, box); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	save(history)
	due := now.Add(-time.Second)
	execSQL("UPDATE rule_task SET due_at=? WHERE device_id=1", due)
	var schema string
	if err = database.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
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
	cmd := exec.CommandContext(ctx, binary, "--rule-scheduler-test")
	cmd.Env = append(os.Environ(), "YIHE_NEXT_DB_HOST="+host, "YIHE_NEXT_DB_PORT="+port, "YIHE_NEXT_DB_SCHEMA="+schema, "YIHE_NEXT_DB_USER="+cfg.User, "YIHE_NEXT_DB_PASSWORD="+cfg.Passwd, "YIHE_NEXT_TEST_EVENT_KEY="+key)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	reader := bufio.NewReader(stdout)
	type outcome struct {
		State                         string
		Committed, Deferred, Fallback int
	}
	var last NativeCollectReply
	var beforeCollect func()
	tick := func(at time.Time, modify func(*NativeCollectReply), want outcome) {
		t.Helper()
		if _, err := fmt.Fprintf(stdin, "%d\n", at.UnixNano()); err != nil {
			t.Fatal(err)
		}
		for {
			raw, err := reader.ReadBytes('\n')
			if err != nil {
				t.Fatal(err)
			}
			var result outcome
			if err = json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if result.State == "tick" {
				if result != want {
					t.Fatalf("tick got %+v want %+v", result, want)
				}
				return
			}
			var req NativeCollectRequest
			if err = json.Unmarshal(raw, &req); err != nil || req.State != "collect" {
				t.Fatalf("collector request invalid: %v", err)
			}
			if beforeCollect != nil {
				f := beforeCollect
				beforeCollect = nil
				f()
			}
			reply, err := CollectNativeRuleInput(ctx, database, box, req)
			if err != nil {
				t.Fatal(err)
			}
			last = reply
			if modify != nil {
				modify(&reply)
			}
			raw, err = json.Marshal(reply)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = stdin.Write(append(raw, '\n')); err != nil {
				t.Fatal(err)
			}
		}
	}
	empty := func() {
		t.Helper()
		for _, table := range []string{"rule_state", "care_incident", "care_incident_event", "care_notification_outbox", "message_outbox", "rust_rule_receipt"} {
			if n := count("SELECT COUNT(*) FROM " + table); n != 0 {
				t.Fatalf("%s partially committed: %d", table, n)
			}
		}
		var got time.Time
		if err := database.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&got); err != nil || !got.Equal(due) {
			t.Fatalf("task lost on rollback: %v %v", got, err)
		}
	}
	deferOne := outcome{State: "tick", Deferred: 1}
	// Actual late-lock writer's owner-clock lock. Rust must not wait for it
	// while owning the device. The collector can run without the lock;
	// the write phase must yield immediately and preserve the task.
	holder, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = health.LockOwner(ctx, holder, 1); err != nil {
		holder.Rollback()
		t.Fatal(err)
	}
	start := time.Now()
	tick(now, nil, deferOne)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("clock conflict blocked: %v", elapsed)
	}
	if err = holder.Rollback(); err != nil {
		t.Fatal(err)
	}
	empty()
	tick(now, func(r *NativeCollectReply) { v := *r.OwnerRevision + 1; r.OwnerRevision = &v }, deferOne)
	empty()
	tick(now, func(r *NativeCollectReply) { v := *r.PolicyRevision - 1; r.PolicyRevision = &v }, deferOne)
	empty()
	tick(now, func(r *NativeCollectReply) { r.Batch.Epoch++ }, deferOne)
	empty()
	tick(now, func(r *NativeCollectReply) { r.Batch.Input.Heart.Value++ }, deferOne)
	empty()
	tick(now, func(r *NativeCollectReply) { r.Challenge = "stale" }, deferOne)
	empty()
	// A write between the Rust read snapshot and the Go read snapshot
	// must produce an ordinary stale reply, not a protocol failure.
	beforeCollect = func() { execSQL("UPDATE device SET battery=7 WHERE device_id=1") }
	tick(now, func(r *NativeCollectReply) {
		if !r.Stale {
			t.Fatal("collector accepted changed snapshot")
		}
	}, deferOne)
	empty()
	execSQL("UPDATE device SET battery=5 WHERE device_id=1")
	// A real writer changing the authoritative state during the IPC
	// interval invalidates the whole read snapshot, even if due_at is
	// already overdue and remains unchanged by the writer trigger.
	tick(now, func(r *NativeCollectReply) {
		execSQL("UPDATE rule_policy SET revision=8 WHERE device_id=1")
	}, deferOne)
	empty()
	tick(now, func(r *NativeCollectReply) {
		execSQL("UPDATE device SET battery=6 WHERE device_id=1")
	}, deferOne)
	empty()
	execSQL("UPDATE device SET battery=5 WHERE device_id=1")
	// Commit a correction after the collector has computed the old value.
	// Only the owner revision changes, not the selected sample identity.
	tick(now, func(r *NativeCollectReply) {
		extra := sample("revision-fence-extra", "heart_rate", "count/min", 80, now.Add(-2*time.Hour))
		save([]health.Sample{extra})
	}, deferOne)
	empty()
	tick(now, func(r *NativeCollectReply) {
		execSQL("INSERT INTO ratepre(device_id,old_id,hr,create_time) VALUES(1,1,151,?)", now.Add(-time.Second))
	}, deferOne)
	empty()
	// Roll back at the last SQL operation, after all seven rule states,
	// incidents, both notifications and the receipt were prepared.
	execSQL("CREATE TRIGGER rust_fail_schedule BEFORE UPDATE ON rule_task FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
	tick(now, nil, deferOne)
	empty()
	execSQL("DROP TRIGGER rust_fail_schedule")
	// All these rows must be acquirable while the Rust process waits for
	// collector IPC. Actual current data is revalidated after the reply.
	tick(now, func(r *NativeCollectReply) {
		for _, q := range []string{"SELECT revision FROM health_owner_clock WHERE old_id=1 FOR UPDATE NOWAIT", "SELECT revision FROM rule_policy WHERE device_id=1 FOR UPDATE NOWAIT", "SELECT health_epoch FROM device WHERE device_id=1 FOR UPDATE NOWAIT", "SELECT epoch FROM rule_task WHERE device_id=1 FOR UPDATE NOWAIT"} {
			tx, err := database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			var value int64
			err = tx.QueryRowContext(ctx, q).Scan(&value)
			_ = tx.Rollback()
			if err != nil {
				t.Fatalf("collector held a write fence for %s: %v", q, err)
			}
		}
	}, outcome{State: "tick", Committed: 1})
	if last.Batch == nil || len(last.Batch.Evidence) != 7 {
		t.Fatalf("expected seven actual Go rule candidates, got %+v", last.Batch)
	}
	for _, table := range []string{"rule_state", "care_incident", "care_incident_event", "care_notification_outbox", "message_outbox"} {
		if n := count("SELECT COUNT(*) FROM " + table); n != 7 {
			t.Fatalf("%s expected seven rows got %d", table, n)
		}
	}
	if n := count("SELECT COUNT(*) FROM rust_rule_receipt"); n != 1 {
		t.Fatalf("receipt count %d", n)
	}
	for _, candidate := range last.Batch.Evidence {
		var observation string
		var streak int
		var active bool
		var raw []byte
		if err = database.QueryRowContext(ctx, "SELECT observation_key,streak,active,evidence FROM rule_state WHERE device_id=1 AND epoch=1 AND kind=?", candidate.Kind).Scan(&observation, &streak, &active, &raw); err != nil {
			t.Fatal(err)
		}
		if observation != candidate.Key || streak != 1 || !active {
			t.Fatalf("native state mismatch %s", candidate.Kind)
		}
		var wrapped map[string]string
		if err = json.Unmarshal(raw, &wrapped); err != nil {
			t.Fatal(err)
		}
		plain, err := box.Open(wrapped["encrypted"], "rule-evidence")
		if err != nil {
			t.Fatal(err)
		}
		var got, want map[string]any
		if err = json.Unmarshal([]byte(plain), &got); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(candidate.Data, &want); err != nil {
			t.Fatal(err)
		}
		want["streak"] = float64(1)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("protected rule evidence differs for %s: %+v %+v", candidate.Kind, got, want)
		}
	}
	var next time.Time
	if err = database.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&next); err != nil || !next.Equal(time.Unix(0, *last.NextDueNS)) {
		t.Fatalf("actual next due mismatch: %v %v", next, err)
	}
	tick(now, nil, outcome{State: "tick"})
	// Committed health correction wakes the task through the actual device
	// heartbeat trigger; native re-evaluation reads its new revision.
	heart.Version = 2
	heart.Value = health.Number(80)
	save([]health.Sample{heart})
	// Fixture clock is historical; emulate the actual wakeup's due instant.
	execSQL("UPDATE rule_task SET due_at=? WHERE device_id=1", next)
	tick(next, nil, outcome{State: "tick", Committed: 1})
	if n := count("SELECT COUNT(*) FROM rule_state WHERE kind='heart_rate' AND NOT active AND streak=0"); n != 1 {
		t.Fatal("corrected authoritative observation did not clear rule")
	}
	if n := count("SELECT COUNT(*) FROM care_incident"); n != 7 {
		t.Fatalf("correction duplicated incidents: %d", n)
	}
	// Disabled policy is a durable no-work state, with the normal due row
	// cleared in the same transaction rather than polled forever.
	p.Enabled = false
	payload, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	execSQL("UPDATE rule_policy SET revision=8,payload=? WHERE device_id=1", payload)
	execSQL("UPDATE rule_task SET due_at=? WHERE device_id=1", next.Add(time.Second))
	tick(next.Add(time.Second), nil, outcome{State: "tick", Committed: 1})
	if n := count("SELECT COUNT(*) FROM rule_task WHERE device_id=1 AND due_at IS NULL"); n != 1 {
		t.Fatal("disabled task not disarmed")
	}
	// Start an actual Go SOS business transaction while Rust awaits its
	// adapter response. The fixture's nonce and no-position persistence
	// hooks are local; the real SOS locks, authentication, commit and
	// notification workflow execute against the same MySQL database.
	// This is not an HTTP or full-service latency benchmark.
	// Active night SOS upgrade remains on Go, without consuming its task.
	p.Enabled = true
	payload, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	execSQL("UPDATE rule_policy SET revision=9,payload=? WHERE device_id=1", payload)
	execSQL("UPDATE rule_task SET due_at=? WHERE device_id=1", next.Add(2*time.Second))
	sosNow := next.Add(2 * time.Second)
	// Deliberately withhold the collector response. SOS must finish
	// without waiting for it; the old implementation times out here.
	var sosElapsed time.Duration
	tick(sosNow, func(r *NativeCollectReply) {
		time.Sleep(250 * time.Millisecond)
		sosCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		started := time.Now()
		result, failure, err := deviceworkflow.SOS(sosCtx, database.SOSPool(), box, deviceworkflow.SOSInput{Number: "RULE001", Secret: sosSecret, Epoch: 1, RequestID: "ipc-sos"}, deviceworkflow.SOSHooks{
			AcceptNonce: func(context.Context, *sql.Tx, int64) bool { return true },
			Persist: func(ctx context.Context, tx *sql.Tx, device *devicerepo.DeviceRecord) (int64, error) {
				res, err := tx.ExecContext(ctx, "INSERT INTO care_incident(old_id,device_id,kind,severity,request_id) VALUES(?,?,'sos','high','ipc-sos')", *device.OldID, device.ID)
				if err != nil {
					return 0, err
				}
				return res.LastInsertId()
			},
		})
		sosElapsed = time.Since(started)
		if err != nil || failure != "" || result.IncidentID <= 0 {
			t.Fatalf("SOS blocked during collector IPC: %v %s", err, failure)
		}
		time.Sleep(250 * time.Millisecond)
	}, deferOne)
	if count("SELECT COUNT(*) FROM care_incident WHERE kind='sos' AND request_id='ipc-sos'") != 1 || count("SELECT COUNT(*) FROM care_notification_outbox n JOIN care_incident i ON i.id=n.incident_id WHERE i.request_id='ipc-sos'") != 1 {
		t.Fatal("SOS durable incident/outbox missing")
	}
	t.Logf("SOS workflow committed during withheld collector reply in %v (500ms deliberate IPC delay)", sosElapsed)
	tick(sosNow, nil, outcome{State: "tick", Fallback: 1})
	if n := count("SELECT COUNT(*) FROM rule_task WHERE device_id=1 AND due_at=?", sosNow); n != 1 {
		t.Fatal("fallback task consumed")
	}
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	// Kill the actual scheduler immediately after its durable COMMIT but
	// before it can report the result. A restarted worker must observe
	// the disarmed task and must not create a second receipt.
	p.Enabled = false
	payload, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	execSQL("UPDATE rule_policy SET revision=10,payload=? WHERE device_id=1", payload)
	crashAt := next.Add(3 * time.Second)
	execSQL("UPDATE rule_task SET due_at=? WHERE device_id=1", crashAt)
	before := count("SELECT COUNT(*) FROM rust_rule_receipt")
	startProcess := func(mode string) (*exec.Cmd, io.WriteCloser, *bufio.Reader) {
		t.Helper()
		c := exec.CommandContext(ctx, binary, mode)
		c.Env = cmd.Env
		c.Stderr = os.Stderr
		in, err := c.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err = c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c, in, bufio.NewReader(out)
	}
	collect := func(in io.Writer, reader *bufio.Reader) {
		t.Helper()
		raw, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var req NativeCollectRequest
		if err = json.Unmarshal(raw, &req); err != nil || req.State != "collect" {
			t.Fatalf("invalid worker request: %v", err)
		}
		reply, err := CollectNativeRuleInput(ctx, database, box, req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = json.Marshal(reply)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = in.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	crashed, crashIn, crashOut := startProcess("--rule-scheduler-crash-test")
	if _, err = fmt.Fprintf(crashIn, "%d\n", crashAt.UnixNano()); err != nil {
		t.Fatal(err)
	}
	collect(crashIn, crashOut)
	if raw, err := crashOut.ReadBytes('\n'); err != io.EOF || len(raw) != 0 {
		t.Fatalf("expected process exit before tick: %v", err)
	}
	_ = crashIn.Close()
	var exit *exec.ExitError
	if err = crashed.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 79 {
		t.Fatalf("unexpected crash result: %v", err)
	}
	if count("SELECT COUNT(*) FROM rust_rule_receipt") != before+1 || count("SELECT COUNT(*) FROM rule_task WHERE device_id=1 AND due_at IS NULL") != 1 {
		t.Fatal("post-commit crash lost durable state")
	}

	worker, workerIn, workerOut := startProcess("--rule-scheduler-worker-test")
	ready, err := workerOut.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var state outcome
	if err = json.Unmarshal(ready, &state); err != nil || state.State != "ready" {
		t.Fatalf("worker not ready: %v", err)
	}
	if count("SELECT COUNT(*) FROM rust_rule_receipt") != before+1 {
		t.Fatal("restart duplicated receipt")
	}
	// Real trigger wakeups: no manual due_at writes or caller-supplied
	// tick. Policy change and device heartbeat each awaken an idle worker.
	for i, wake := range []func(){
		func() { execSQL("UPDATE rule_policy SET revision=11 WHERE device_id=1") },
		func() { execSQL("UPDATE device SET last_time=UTC_TIMESTAMP(6) WHERE device_id=1") },
	} {
		wake()
		collect(workerIn, workerOut)
		raw, err := workerOut.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var got outcome
		if err = json.Unmarshal(raw, &got); err != nil || got != (outcome{State: "tick", Committed: 1}) {
			t.Fatalf("trigger wake %d: %+v %v", i, got, err)
		}
		if count("SELECT COUNT(*) FROM rust_rule_receipt") != before+2+i || count("SELECT COUNT(*) FROM rule_task WHERE device_id=1 AND due_at IS NULL") != 1 {
			t.Fatal("trigger wake did not commit exactly once")
		}
	}
	if _, err = fmt.Fprintln(workerIn, "stop"); err != nil {
		t.Fatal(err)
	}
	if err = workerIn.Close(); err != nil {
		t.Fatal(err)
	}
	if err = worker.Wait(); err != nil {
		t.Fatal(err)
	}
	t.Log("actual protected samples drive seven native rules; revision/policy/epoch/challenge fences, NOWAIT clock contention, atomic task rollback, correction, disarm, Go fallback, post-commit crash and autonomous trigger wakeups passed")
}
