package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"yihe-care/internal/db"
	"yihe-care/internal/db/dbtest"
	"yihe-care/internal/health"
)

func seed(t *testing.T) (*db.Database, time.Time) {
	t.Helper()
	p := dbtest.Fixture(t)
	now := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC)
	ctx := context.Background()
	if _, err := p.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'长者')"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExecContext(ctx, "INSERT INTO device(device_id,device_no,secret,old_id,last_time,bind_time) VALUES(1,'RULE001','unused',1,?,?)", now, now); err != nil {
		t.Fatal(err)
	}
	return p, now
}
func save(t *testing.T, p *db.Database, s health.Sample) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	id := int64(1)
	if _, _, err = health.Save(ctx, tx, health.Owner{OldID: 1, DeviceID: &id, Epoch: 1}, s); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func TestBaselineSourceIndexPreservesExactIdentity(t *testing.T) {
	for _, prefix := range []int{16, 64} {
		t.Run(fmt.Sprint(prefix), func(t *testing.T) { testBaselineSourceIdentity(t, prefix) })
	}
}

func testBaselineSourceIdentity(t *testing.T, devicePrefix int) {
	p, now := seed(t)
	ctx := context.Background()
	// Index prefixes and case-insensitive legacy collations are only filters;
	// neither may merge distinct sources into a baseline.
	if _, err := p.ExecContext(ctx, "ALTER TABLE health_sample MODIFY source varchar(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL, MODIFY source_device varchar(4096) CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci NOT NULL"); err != nil {
		t.Fatal(err)
	}
	source, device := strings.Repeat("s", 64)+"a", strings.Repeat("d", devicePrefix)+"a"
	for group, identity := range [][2]string{{source, device}, {strings.ToUpper(source), device}, {source, strings.ToUpper(device)}, {source[:64] + "b", device}, {source, device[:len(device)-1] + "b"}} {
		value := 150.0
		if group == 0 {
			value = 70
		}
		for i := range 20 {
			at := now.Add(-time.Duration(i+30) * time.Hour)
			save(t, p, health.Sample{RecordID: fmt.Sprintf("base-%d-%d", group, i), Source: identity[0], SourceDevice: identity[1], Metric: "heart_rate", Unit: "count/min", Value: health.Number(value), Start: at, End: at, Version: 1})
		}
	}
	e := Engine{DB: p}
	for i := range 3 {
		at := now.Add(time.Duration(i) * time.Second)
		save(t, p, health.Sample{RecordID: fmt.Sprint("current", i), Source: source, SourceDevice: device, Metric: "heart_rate", Unit: "count/min", Value: health.Number(100), Start: at, End: at, Version: 1})
		if err := e.Device(ctx, 1, at.Add(time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE kind='rule_baseline_heart_rate'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("source identity changed baseline: incidents=%d err=%v", count, err)
	}
}

func TestBaselineStreakUsesDistinctObservationsAndRecovers(t *testing.T) {
	p, now := seed(t)
	e := Engine{DB: p}
	ctx := context.Background()
	historical := func(i int) health.Sample {
		at := now.Add(-time.Duration(i+30) * time.Hour)
		return health.Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "RULE001", Metric: "heart_rate", Unit: "count/min",
			Value: health.Number(70), Start: at, End: at, Version: 1}
	}
	for i := 0; i < 19; i++ {
		save(t, p, historical(i))
	}
	first := health.Sample{RecordID: "current0", Source: "watch", SourceDevice: "RULE001", Metric: "heart_rate",
		Unit: "count/min", Value: health.Number(100), Start: now, End: now, Version: 1}
	save(t, p, first)
	if err := e.Device(ctx, 1, now.Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM rule_state WHERE kind='baseline_heart_rate'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("insufficient history created baseline state: count=%d err=%v", count, err)
	}
	save(t, p, historical(19))
	if err := e.Device(ctx, 1, now.Add(2*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var streak int
	if err := p.QueryRowContext(ctx, "SELECT streak FROM rule_state WHERE kind='baseline_heart_rate'").Scan(&streak); err != nil || streak != 1 {
		t.Fatalf("late history did not qualify the current observation: streak=%d err=%v", streak, err)
	}
	for i := 1; i < 3; i++ {
		at := now.Add(time.Duration(i) * time.Second)
		reading := first
		reading.RecordID, reading.Start, reading.End = fmt.Sprint("current", i), at, at
		save(t, p, reading)
		if err := e.Device(ctx, 1, at.Add(time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE kind='rule_baseline_heart_rate'").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := e.Device(ctx, 1, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident").Scan(&count)
	if count != 1 {
		t.Fatal("repeated observation emitted again", count)
	}
	at := now.Add(2 * time.Second)
	save(t, p, health.Sample{RecordID: "current2", Source: "watch", SourceDevice: "RULE001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(70), Start: at, End: at, Version: 2})
	if err := e.Device(ctx, 1, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	var active bool
	p.QueryRowContext(ctx, "SELECT active FROM rule_state WHERE kind='baseline_heart_rate'").Scan(&active)
	if active {
		t.Fatal("corrected healthy observation did not reset rule")
	}
}

func TestBaselineCoverUsesIndexWithoutPayloadLookups(t *testing.T) {
	p, now := seed(t)
	for i := range 20 {
		at := now.Add(-time.Duration(i+30) * time.Hour)
		save(t, p, health.Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "RULE001", Metric: "heart_rate", Unit: "count/min", Value: health.Number(70), Start: at, End: at, Version: 1})
	}
	args := []any{int64(1), int64(1), "heart_rate", "watch", baselineSourceKey("RULE001"), "watch", now.AddDate(0, 0, -7).UnixMicro(), now.Add(-15 * time.Minute).UnixMicro()}
	var count, values int
	var mean, deviation float64
	if err := p.QueryRowContext(context.Background(), baselineCoverSQL, args...).Scan(&count, &values, &mean, &deviation); err != nil || count != 20 || values != 20 || mean != 70 || deviation != 0 {
		t.Fatalf("cover changed baseline: count=%d mean=%g deviation=%g err=%v", count, mean, deviation, err)
	}
	var raw string
	if err := p.QueryRowContext(context.Background(), "EXPLAIN FORMAT=JSON "+baselineCoverSQL, args...).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan any
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		t.Fatal(err)
	}
	var covered func(any) bool
	covered = func(value any) bool {
		switch value := value.(type) {
		case map[string]any:
			if value["key"] == "idx_health_rule_cover" && value["using_index"] == true {
				return true
			}
			for _, child := range value {
				if covered(child) {
					return true
				}
			}
		case []any:
			for _, child := range value {
				if covered(child) {
					return true
				}
			}
		}
		return false
	}
	if !covered(plan) {
		t.Fatalf("baseline requires clustered payload reads: %s", raw)
	}
}
func TestNightActivityCounterResetAndConcurrentOfflineDedup(t *testing.T) {
	p, now := seed(t)
	ctx := context.Background()
	e := Engine{DB: p}
	for i, value := range []float64{1000, 5, 400} {
		at := now.Add(time.Duration(i-12) * time.Minute)
		save(t, p, health.Sample{RecordID: fmt.Sprint("steps", i), Source: "watch", SourceDevice: "RULE001", Metric: "steps", Semantics: "daily_total", Unit: "count", Value: health.Number(value), Start: at, End: at, Version: 1})
	}
	if err := e.Device(ctx, 1, now); err != nil {
		t.Fatal(err)
	}
	var count int
	p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident").Scan(&count)
	if count != 0 {
		t.Fatal("counter reset became activity", count)
	}
	p.ExecContext(ctx, "UPDATE device SET last_time=?", now.Add(-time.Hour))
	var wg sync.WaitGroup
	fail := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); engine := Engine{DB: p}; fail <- engine.Device(ctx, 1, now) }()
	}
	wg.Wait()
	close(fail)
	for err := range fail {
		if err != nil {
			t.Fatal(err)
		}
	}
	p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE kind='rule_no_report'").Scan(&count)
	if count != 1 {
		t.Fatal("offline deduplication", count)
	}
}

func TestNightActivityMonotonicAndIntervals(t *testing.T) {
	for _, semantics := range []string{"daily_total", "interval"} {
		t.Run(semantics, func(t *testing.T) {
			p, now := seed(t)
			for i, value := range []float64{100, 400} {
				at := now.Add(time.Duration(i-12) * time.Minute)
				end := at
				if semantics == "interval" {
					end = at.Add(30 * time.Second)
				}
				save(t, p, health.Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "RULE001", Metric: "steps", Semantics: semantics, Unit: "count", Value: health.Number(value), Start: at, End: end, Version: 1})
			}
			e := Engine{DB: p}
			for range 2 {
				if err := e.Device(context.Background(), 1, now); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err := p.QueryRowContext(context.Background(), "SELECT count(*) FROM care_incident WHERE kind='rule_night_activity'").Scan(&count); err != nil || count != 1 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestTickDuePagesAndRetriesFailedDevice(t *testing.T) {
	p, now := seed(t)
	ctx := context.Background()
	for id := 2; id <= 102; id++ {
		if _, err := p.ExecContext(ctx, "INSERT INTO device(device_id,device_no,secret,old_id,last_time,bind_time) VALUES(?,?,'unused',1,?,?)", id, fmt.Sprintf("RULE%03d", id), now, now); err != nil {
			t.Fatal(err)
		}
	}
	policy := Default()
	policy.Enabled = false
	payload, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "INSERT INTO rule_policy(device_id,revision,payload,updated_by) SELECT device_id,1,?,1 FROM device", json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: p}
	if err = e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var dormant int
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM rule_task WHERE due_at IS NULL").Scan(&dormant); err != nil || dormant != 102 {
		t.Fatalf("dormant tasks=%d err=%v", dormant, err)
	}
	policy.Consecutive = 0
	invalid, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE rule_policy SET payload=? WHERE device_id=50", json.RawMessage(invalid)); err != nil {
		t.Fatal(err)
	}
	live, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE rule_policy SET payload=? WHERE device_id=75", json.RawMessage(live)); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE device SET last_time=? WHERE device_id=75", time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx); err == nil {
		t.Fatal("invalid policy did not fail")
	}
	var pending bool
	if err = p.QueryRowContext(ctx, "SELECT due_at IS NOT NULL FROM rule_task WHERE device_id=50").Scan(&pending); err != nil || !pending {
		t.Fatal("failed task lost", pending, err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE rule_policy SET payload=? WHERE device_id=50", json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	if err = e.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRowContext(ctx, "SELECT due_at IS NOT NULL FROM rule_task WHERE device_id=50").Scan(&pending); err != nil || pending {
		t.Fatal("retry did not finish", pending, err)
	}
	var count int
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM rule_state").Scan(&count); err != nil || count != 1 {
		t.Fatal("disabled policies created state", count, err)
	}
	if err = p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE device_id=75 AND kind='rule_no_report'").Scan(&count); err != nil || count != 1 {
		t.Fatal("retry duplicated successful device incident", count, err)
	}
}

func TestNightIncidentUpgradeRemainsIdempotent(t *testing.T) {
	for _, kind := range []string{"sos", "fence_exit"} {
		t.Run(kind, func(t *testing.T) {
			p, now := seed(t)
			ctx := context.Background()
			if _, err := p.ExecContext(ctx, "INSERT INTO care_incident(old_id,device_id,kind,severity,created_at) VALUES(1,1,?,'high',?)", kind, now); err != nil {
				t.Fatal(err)
			}
			e := Engine{DB: p}
			for range 2 {
				if err := e.Device(ctx, 1, now); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			var severity string
			if err := p.QueryRowContext(ctx, "SELECT count(*),max(severity) FROM care_incident").Scan(&count, &severity); err != nil || count != 1 || severity != "critical" {
				t.Fatal(count, severity, err)
			}
			if err := p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident_event WHERE action='night_rule'").Scan(&count); err != nil || count != 1 {
				t.Fatal("duplicate upgrade", count, err)
			}
		})
	}
}
