//go:build yihe_next_gateway

package rules

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
)

// Read actual Go-committed protected health_sample references, the owner
// cursor (not the global reserved range), and the authoritative rule policy.
// This is a read-only bridge; it does not advance or claim rule_task.
func TestNextCoreRuleAuthority(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Fatal("YIHE_NEXT_GATEWAY_BINARY required")
	}
	db, now := seed(t)
	ctx := t.Context()
	sqlExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	sqlExec("UPDATE device SET status=1,battery=19,last_time=? WHERE device_id=1", now)
	sqlExec("INSERT INTO watch_info(device_id,hrf,hrl,bpf,bpl) VALUES(1,105,60,145,95)")
	sqlExec("UPDATE rule_task SET due_at=? WHERE device_id=1", now.Add(-time.Second))
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{29}, 32))
	box, err := securestore.New(key)
	if err != nil {
		t.Fatal(err)
	}
	device := int64(1)
	at := now.Add(-time.Second)
	heart := health.Sample{RecordID: "authority-hr", Source: "watch", SourceDevice: "RULE001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(75), Start: at, End: at, Version: 1}
	blood := health.Sample{RecordID: "authority-bp", Source: "watch", SourceDevice: "RULE001", Metric: "blood_pressure_systolic", Unit: "mmHg", Value: health.Number(120), Start: at, End: at, Version: 1}
	steps := health.Sample{RecordID: "authority-step", Source: "watch", SourceDevice: "RULE001", Metric: "steps", Unit: "count", Semantics: "interval", Value: health.Number(42), Start: at.Add(-time.Minute), End: at, Version: 1}
	save := func(samples []health.Sample, commit bool) {
		t.Helper()
		batch, err := health.PrepareBatch(samples)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = batch.SaveProtected(ctx, tx, health.Owner{OldID: 1, DeviceID: &device, Epoch: 1}, box); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	save([]health.Sample{heart, blood, steps}, true)
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
	cmd := exec.CommandContext(ctx, binary, "--rule-authority-test")
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
	type observation struct {
		Metric       string  `json:"metric"`
		Key          string  `json:"key"`
		Revision     int64   `json:"revision"`
		Value        float64 `json:"value"`
		Source       string  `json:"source"`
		SourceDevice string  `json:"sourceDevice"`
		AtNs         int64   `json:"atNs"`
	}
	type snapshot struct {
		State          string        `json:"state"`
		Owner          int64         `json:"owner"`
		Epoch          int64         `json:"epoch"`
		OwnerRevision  *int64        `json:"ownerRevision"`
		PolicyRevision *int64        `json:"policyRevision"`
		Policy         Policy        `json:"policy"`
		Observations   []observation `json:"observations"`
		LastReportNs   *int64        `json:"lastReportNs"`
		Battery        *float64      `json:"battery"`
		TaskDue        bool          `json:"taskDue"`
	}
	read := func() snapshot {
		t.Helper()
		if _, err = fmt.Fprintf(stdin, "[1,%d]\n", now.UnixNano()); err != nil {
			t.Fatal(err)
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var got snapshot
		if err = json.Unmarshal(line, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	check := func() snapshot {
		t.Helper()
		got := read()
		if got.State != "" || got.Owner != 1 || got.Epoch != 1 || got.OwnerRevision == nil || got.LastReportNs == nil || *got.LastReportNs != now.UnixNano() || got.Battery == nil || *got.Battery != 19 {
			t.Fatalf("authority identity unavailable: %+v", got)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		p, err := readRulePolicy(ctx, tx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Policy, p) {
			t.Fatalf("Go/Rust policy differs: %+v %+v", p, got.Policy)
		}
		var ownerRevision int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM health_owner_clock WHERE old_id=1").Scan(&ownerRevision); err != nil || ownerRevision != *got.OwnerRevision {
			t.Fatalf("owner cursor: %d %v", ownerRevision, err)
		}
		var policyRevision *int64
		if got.PolicyRevision != nil {
			if err = tx.QueryRowContext(ctx, "SELECT revision FROM rule_policy WHERE device_id=1").Scan(&policyRevision); err != nil || !reflect.DeepEqual(got.PolicyRevision, policyRevision) {
				t.Fatalf("policy revision: %v", err)
			}
		}
		// Go intentionally omits activity outside night; force night only for
		// raw latest selection, leaving the configured policy check unchanged.
		loc, err := time.LoadLocation(p.Timezone)
		if err != nil {
			t.Fatal(err)
		}
		h, m, s := now.In(loc).Clock()
		p.NightBegin = h*3600 + m*60 + s
		p.NightEnd = (p.NightBegin + 1) % 86400
		in := Input{Now: now}
		if err = readLatestRuleObservations(ctx, tx, ruleDevice{id: 1, oldID: 1, epoch: 1}, p, &in, box); err != nil {
			t.Fatal(err)
		}
		var expected []observation
		for _, v := range []struct {
			metric string
			value  *Value
		}{{"heart_rate", in.Heart}, {"blood_pressure_systolic", in.Blood}, {"steps", in.Activity}} {
			if v.value == nil {
				continue
			}
			var rev int64
			if err = tx.QueryRowContext(ctx, "SELECT revision FROM health_sample WHERE CONCAT(LOWER(HEX(sample_id)),':',revision)=?", v.value.Key).Scan(&rev); err != nil {
				t.Fatal(err)
			}
			expected = append(expected, observation{v.metric, v.value.Key, rev, v.value.Value, v.value.Source, v.value.Device, v.value.At.UnixNano()})
		}
		if !reflect.DeepEqual(got.Observations, expected) {
			t.Fatalf("authority latest differs: Go %+v Rust %+v", expected, got.Observations)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := check()
	if len(first.Observations) != 3 || first.Policy.HRLow != 60 || first.Policy.HRHigh != 105 || !first.TaskDue {
		t.Fatalf("unexpected initial snapshot %+v", first)
	}
	var refs int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM health_sample WHERE payload_cipher=0x01").Scan(&refs); err != nil || refs != 3 {
		t.Fatalf("actual protected references missing: %d %v", refs, err)
	}
	// Existing inline protected payload remains readable too.
	id := first.Observations[0].Key[:64]
	rev := first.Observations[0].Revision
	var stored health.ProjectionPayload
	if err = db.QueryRowContext(ctx, "SELECT payload,"+health.SampleCipherSQL+" FROM health_sample WHERE sample_id=UNHEX(?)", id).Scan(&stored.JSON, &stored.Cipher); err != nil {
		t.Fatal(err)
	}
	raw, err := stored.OpenSample(box, id, 1, rev)
	if err != nil {
		t.Fatal(err)
	}
	inline, err := box.SealBytes(raw, health.SamplePayloadScope(id, "1", fmt.Sprint(rev)))
	if err != nil {
		t.Fatal(err)
	}
	sqlExec("UPDATE health_sample SET payload_cipher=? WHERE sample_id=UNHEX(?)", inline, id)
	check()
	heart.Version = 2
	heart.Value = health.Number(95)
	save([]health.Sample{heart}, false)
	if rolled := check(); !reflect.DeepEqual(rolled, first) {
		t.Fatal("rolled-back version leaked into committed snapshot")
	}
	save([]health.Sample{heart}, true)
	corrected := check()
	if *corrected.OwnerRevision <= *first.OwnerRevision || corrected.Observations[0].Value != 95 || corrected.Observations[0].Key == first.Observations[0].Key {
		t.Fatal("correction did not advance authoritative revision")
	}
	p := Default()
	p.HRHigh = 99
	p.ActivityMinutes = 10
	p.Timezone = "UTC"
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	sqlExec("INSERT INTO rule_policy(device_id,revision,payload,updated_by) VALUES(1,7,?,1)", payload)
	sqlExec("UPDATE rule_task SET due_at=? WHERE device_id=1", now.Add(time.Minute))
	custom := check()
	if custom.PolicyRevision == nil || *custom.PolicyRevision != 7 || custom.Policy.HRHigh != 99 || custom.TaskDue {
		t.Fatal("actual policy/task revision mismatch")
	}
	// Invalid policy is unavailable; do not silently fall back.
	sqlExec("UPDATE rule_policy SET payload=JSON_SET(payload,'$.consecutive',0) WHERE device_id=1")
	if got := read(); got.State != "unavailable" {
		t.Fatal("invalid policy accepted")
	}
	sqlExec("UPDATE rule_policy SET payload=? WHERE device_id=1", payload)
	sqlExec("DELETE FROM health_change WHERE revision=?", corrected.Observations[0].Revision)
	if got := read(); got.State != "unavailable" {
		t.Fatal("missing protected history accepted")
	}
	if err = stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	t.Log("actual Go protected history and inline samples, owner revision, rollback/correction, custom policy/due state and missing-history fail-closed passed")
}
