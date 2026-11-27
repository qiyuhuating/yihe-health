//go:build yihe_next_gateway

package rules

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/securestore"
	"yihe-care/internal/userphone"
)

// Compare actual Go persistence and native Rust evaluation/persistence
// against identical trusted snapshots in one isolated migrated schema.
func TestNextCoreRulePersistence(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Fatal("YIHE_NEXT_GATEWAY_BINARY required")
	}
	db, now := seed(t)
	ctx := t.Context()
	execSQL := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32))
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
	execSQL("UPDATE device SET status=1 WHERE device_id=1")
	execSQL("INSERT INTO device(device_id,device_no,secret,old_id,status,bind_time) VALUES(2,'RULE002','unused',1,1,?)", now)
	execSQL("CREATE TABLE rust_rule_receipt(device_id BIGINT NOT NULL,epoch BIGINT NOT NULL,request_id VARBINARY(64) NOT NULL,payload_digest BINARY(64) NOT NULL,PRIMARY KEY(device_id,epoch,request_id)) ENGINE=InnoDB")
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
	cmd := exec.CommandContext(ctx, binary, "--rule-commit-test")
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
	run := func(raw []byte, want string) {
		t.Helper()
		if _, err = stdin.Write(append(append([]byte(nil), raw...), '\n')); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadString('\n')
		if err != nil || line != want+"\n" && line != want+"\r\n" {
			t.Fatalf("rule transaction output %q want %q: %v", line, want, err)
		}
	}
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err = db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	p := Default()
	for !p.Night(now) {
		now = now.Add(time.Hour)
	}
	engine := Engine{DB: db, Box: box}
	makeBatch := func(i int, normal bool, source string) ([]byte, []CompactCandidate, time.Time, Input) {
		at := now.Add(time.Duration(i) * time.Second)
		h, b, bat, act := 140., 180., 5., 400.
		last := at.Add(-600 * time.Second)
		if normal {
			h, b, bat, act = 80, 120, 90, 0
			last = at
		}
		v := func(value float64, kind string) *Value {
			return &Value{Value: value, Key: fmt.Sprintf("%s-%d", kind, i), Source: source, Device: "fixture", At: at}
		}
		in := Input{Now: at, Heart: v(h, "hr"), Blood: v(b, "bp"), Battery: v(bat, "battery"), Activity: v(act, "activity"), LastReport: last, Baselines: map[string]Baseline{"heart_rate": {Count: 30, Mean: 80, Deviation: 4}, "blood_pressure": {Count: 30, Mean: 120, Deviation: 6}}}
		var scratch [7]CompactCandidate
		candidates := EvaluateCompactInto(scratch[:0], p, in)
		if len(candidates) != 7 {
			t.Fatalf("expected seven candidates, got %d", len(candidates))
		}
		evidence := make([]map[string]any, 0, 7)
		for _, c := range candidates {
			raw, err := c.marshalEvidence(0, 1, at)
			if err != nil {
				t.Fatal(err)
			}
			var data map[string]any
			if err = json.Unmarshal(raw, &data); err != nil {
				t.Fatal(err)
			}
			evidence = append(evidence, map[string]any{"kind": c.Kind, "key": c.Key, "data": data})
		}
		obs := func(v *Value) any { return map[string]any{"value": v.Value, "at_ns": v.At.UnixNano()} }
		base := func(b Baseline) any {
			return map[string]any{"count": b.Count, "mean": b.Mean, "deviation": b.Deviation}
		}
		input := map[string]any{"enabled": p.Enabled, "night": p.Night(at), "activity_night": p.Night(in.Activity.At), "now_ns": at.UnixNano(), "last_report_ns": last.UnixNano(), "heart": obs(in.Heart), "blood": obs(in.Blood), "battery": obs(in.Battery), "activity": obs(in.Activity), "heart_baseline": base(in.Baselines["heart_rate"]), "blood_baseline": base(in.Baselines["blood_pressure"])}
		raw, err := json.Marshal(map[string]any{"device": 2, "owner": 1, "epoch": 1, "request_id": fmt.Sprintf("batch-%d", i), "input": input, "evidence": evidence})
		if err != nil {
			t.Fatal(err)
		}
		return raw, candidates, at, in
	}
	type persisted struct {
		Key      string
		Streak   int
		Active   bool
		Incident bool
		Evidence map[string]any
	}
	state := func(device int, kind string) persisted {
		t.Helper()
		var out persisted
		var incident *int64
		var data []byte
		if err = db.QueryRowContext(ctx, "SELECT observation_key,streak,active,incident_id,evidence FROM rule_state WHERE device_id=? AND epoch=1 AND kind=?", device, kind).Scan(&out.Key, &out.Streak, &out.Active, &incident, &data); err != nil {
			t.Fatal(err)
		}
		out.Incident = incident != nil
		var outer map[string]string
		if err = json.Unmarshal(data, &outer); err != nil {
			t.Fatal(err)
		}
		plain, err := box.Open(outer["encrypted"], "rule-evidence")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal([]byte(plain), &out.Evidence); err != nil {
			t.Fatal(err)
		}
		return out
	}
	check := func(candidates []CompactCandidate) {
		t.Helper()
		for _, c := range candidates {
			if a, b := state(1, c.Kind), state(2, c.Kind); !reflect.DeepEqual(a, b) {
				t.Fatalf("rule %s: Go=%+v Rust=%+v", c.Kind, a, b)
			}
			for _, q := range []string{
				"SELECT COUNT(*) FROM care_incident WHERE device_id=? AND kind=?",
				"SELECT COUNT(*) FROM care_incident_event e JOIN care_incident i ON i.id=e.incident_id WHERE i.device_id=? AND i.kind=?",
				"SELECT COUNT(*) FROM care_notification_outbox n JOIN care_incident i ON i.id=n.incident_id WHERE i.device_id=? AND i.kind=?",
				"SELECT COUNT(*) FROM message_outbox n JOIN care_incident i ON CAST(i.id AS CHAR)=n.source_key WHERE n.topic='care' AND i.device_id=? AND i.kind=?",
			} {
				if a, b := count(q, 1, "rule_"+c.Kind), count(q, 2, "rule_"+c.Kind); a != b {
					t.Fatalf("rule %s notification/incident parity: %d != %d", c.Kind, a, b)
				}
			}
		}
	}
	for i := 0; i < 8; i++ {
		normal := i == 3
		source := "watch"
		if i >= 6 {
			source = "other"
		}
		raw, candidates, at, _ := makeBatch(i, normal, source)
		if i == 0 || i == 2 {
			before := count("SELECT COUNT(*) FROM care_incident WHERE device_id=2")
			receipts := count("SELECT COUNT(*) FROM rust_rule_receipt")
			execSQL("CREATE TRIGGER rust_fail_rule_receipt BEFORE INSERT ON rust_rule_receipt FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected'")
			run(raw, "rejected")
			if count("SELECT COUNT(*) FROM care_incident WHERE device_id=2") != before || count("SELECT COUNT(*) FROM rust_rule_receipt") != receipts {
				t.Fatal("rule rollback failed")
			}
			if i == 0 && count("SELECT COUNT(*) FROM rule_state WHERE device_id=2") != 0 {
				t.Fatal("rule state did not roll back")
			}
			if i == 2 {
				check(candidates)
			}
			execSQL("DROP TRIGGER rust_fail_rule_receipt")
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range candidates {
			if err = engine.persist(ctx, tx, 1, 1, 1, c, at, nil); err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		run(raw, "committed")
		check(candidates)
		run(raw, "committed")
		check(candidates)
		var changed map[string]any
		if err = json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		changed["evidence"].([]any)[0].(map[string]any)["key"] = "conflicting-observation"
		conflict, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		run(conflict, "rejected")
		check(candidates)
	}
	if n := count("SELECT COUNT(DISTINCT kind) FROM care_incident WHERE device_id=2"); n != 7 {
		t.Fatalf("not all seven actual rules triggered: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM rust_rule_receipt"); n != 8 {
		t.Fatalf("wrong receipt count %d", n)
	}
	// Opt-in measured transaction workload, not a production backend
	// comparison. Go is in-process; Rust includes a local pipe round trip.
	// Both evaluate the same inputs and persist all seven states under
	// resident/device locks and a durable experimental receipt.
	if os.Getenv("YIHE_NEXT_RULE_BENCH") == "1" {
		const n = 128
		type item struct {
			raw   []byte
			at    time.Time
			input Input
			id    string
		}
		for round := 0; round < 3; round++ {
			items := make([]item, 0, n)
			for j := 0; j < n; j++ {
				i := 8 + round*n + j
				raw, _, at, input := makeBatch(i, j%4 == 0, "other")
				items = append(items, item{raw, at, input, fmt.Sprintf("batch-%d", i)})
			}
			measure := func(rust bool) {
				t.Helper()
				times := make([]time.Duration, 0, n)
				start := time.Now()
				for _, it := range items {
					begin := time.Now()
					if rust {
						run(it.raw, "committed")
					} else {
						tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
						if err != nil {
							t.Fatal(err)
						}
						var owner, epoch int64
						var status int
						if err = tx.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=1 FOR UPDATE").Scan(&owner); err != nil {
							tx.Rollback()
							t.Fatal(err)
						}
						if err = tx.QueryRowContext(ctx, "SELECT old_id,health_epoch,status FROM device WHERE device_id=1 FOR UPDATE").Scan(&owner, &epoch, &status); err != nil || owner != 1 || epoch != 1 || status != 1 {
							tx.Rollback()
							t.Fatalf("benchmark binding: %v", err)
						}
						var prior string
						if err = tx.QueryRowContext(ctx, "SELECT payload_digest FROM rust_rule_receipt WHERE device_id=1 AND epoch=1 AND request_id=? FOR UPDATE", it.id).Scan(&prior); err != sql.ErrNoRows {
							tx.Rollback()
							t.Fatalf("unexpected benchmark receipt: %v", err)
						}
						var scratch [7]CompactCandidate
						var evidence [512]byte
						for _, c := range EvaluateCompactInto(scratch[:0], p, it.input) {
							if err = engine.persist(ctx, tx, 1, 1, 1, c, it.at, evidence[:0]); err != nil {
								tx.Rollback()
								t.Fatal(err)
							}
						}
						digest := box.Index("yihe-next-rule-batch-v1", string(it.raw))
						if _, err = tx.ExecContext(ctx, "INSERT INTO rust_rule_receipt(device_id,epoch,request_id,payload_digest) VALUES(1,1,?,?)", it.id, digest); err != nil {
							tx.Rollback()
							t.Fatal(err)
						}
						if err = tx.Commit(); err != nil {
							t.Fatal(err)
						}
					}
					times = append(times, time.Since(begin))
				}
				elapsed := time.Since(start)
				sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
				name := "Go"
				if rust {
					name = "Rust"
				}
				t.Logf("RULE_BENCH round=%d runtime=%s batches=%d batches/s=%.2f p50_us=%.2f p99_us=%.2f", round+1, name, n, float64(n)/elapsed.Seconds(), float64(times[n/2].Nanoseconds())/1000, float64(times[(n*99+99)/100-1].Nanoseconds())/1000)
			}
			if round%2 == 0 {
				measure(false)
				measure(true)
			} else {
				measure(true)
				measure(false)
			}
			var scratch [7]CompactCandidate
			check(EvaluateCompactInto(scratch[:0], p, items[n-1].input))
		}
		if n := count("SELECT COUNT(*) FROM rust_rule_receipt WHERE device_id=1"); n != 3*128 {
			t.Fatalf("Go benchmark receipts %d", n)
		}
		if n := count("SELECT COUNT(*) FROM rust_rule_receipt WHERE device_id=2"); n != 8+3*128 {
			t.Fatalf("Rust benchmark receipts %d", n)
		}
	}
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	t.Log("56 native rule candidates and durable states match actual Go transactions, all seven incident/notification kinds activated, receipt rollback and duplicate retry passed")
}
