package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"yihe-care/internal/health"
)

func TestRuleTaskNightBoundaryDST(t *testing.T) {
	p := Default()
	p.Timezone = "America/New_York"
	p.NightBegin = 2*3600 + 30*60
	p.NightEnd = 5 * 3600
	spring := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)
	if got := nextNightBoundary(p, spring.Add(-time.Second)); !got.Equal(spring) {
		t.Fatal("missed spring gap", got)
	}
	if !p.Night(spring) || p.Night(spring.Add(-time.Second)) {
		t.Fatal("spring rule transition")
	}
	if got := nextNightBoundary(p, spring); !got.Equal(spring.Add(2 * time.Hour)) {
		t.Fatal("wrong post-gap boundary", got)
	}
	p.NightBegin = 90 * 60
	p.NightEnd = 150 * 60
	fall := time.Date(2026, 11, 1, 6, 0, 0, 0, time.UTC)
	if got := nextNightBoundary(p, fall.Add(-time.Second)); !got.Equal(fall) {
		t.Fatal("missed fall fold", got)
	}
	if p.Night(fall) || !p.Night(fall.Add(-time.Second)) {
		t.Fatal("fall rule transition")
	}
	if got := nextNightBoundary(p, fall); !got.Equal(fall.Add(30 * time.Minute)) {
		t.Fatal("wrong repeated hour boundary", got)
	}
}

func TestRuleTaskTransactionAndRebind(t *testing.T) {
	p, now := seed(t)
	ctx := context.Background()
	policy := Default()
	policy.Enabled = false
	payload, _ := json.Marshal(policy)
	if _, err := p.ExecContext(ctx, "INSERT INTO rule_policy(device_id,revision,payload,updated_by) VALUES(1,1,?,1)", json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	if err := (&Engine{DB: p}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	read := func() (int64, bool, *time.Time) {
		t.Helper()
		var epoch int64
		var eligible bool
		var due *time.Time
		if err := p.QueryRowContext(ctx, "SELECT epoch,eligible,due_at FROM rule_task WHERE device_id=1").Scan(&epoch, &eligible, &due); err != nil {
			t.Fatal(err)
		}
		return epoch, eligible, due
	}
	if _, eligible, due := read(); !eligible || due != nil {
		t.Fatal("disabled policy not dormant")
	}
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE device SET last_time=? WHERE device_id=1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var pending bool
	if err = tx.QueryRowContext(ctx, "SELECT due_at IS NOT NULL FROM rule_task WHERE device_id=1").Scan(&pending); err != nil || !pending {
		t.Fatal("no transaction-local wake", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, _, due := read(); due != nil {
		t.Fatal("rolled back wake persisted")
	}
	if _, err = p.ExecContext(ctx, "UPDATE device SET last_time=? WHERE device_id=1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, due := read(); due == nil {
		t.Fatal("committed wake missing")
	}
	if err := (&Engine{DB: p}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, due := read(); due != nil {
		t.Fatal("restart did not drain")
	}
	if _, err = p.ExecContext(ctx, "UPDATE device SET old_id=NULL WHERE device_id=1"); err != nil {
		t.Fatal(err)
	}
	if epoch, eligible, due := read(); epoch != 2 || eligible || due != nil {
		t.Fatal("old binding retained task", epoch, eligible, due)
	}
	if _, err = p.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(2,'second')"); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE device SET old_id=2,bind_time=? WHERE device_id=1", now); err != nil {
		t.Fatal(err)
	}
	if epoch, eligible, due := read(); epoch != 3 || !eligible || due == nil {
		t.Fatal("new binding not scheduled", epoch, eligible, due)
	}
	if err := (&Engine{DB: p}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "INSERT INTO watch_info(device_id,hrf,hrl) VALUES(1,55,110)"); err != nil {
		t.Fatal(err)
	}
	if _, _, due := read(); due == nil {
		t.Fatal("threshold write did not wake")
	}
	if _, err = p.ExecContext(ctx, "DELETE FROM device WHERE device_id=1"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = p.QueryRowContext(ctx, "SELECT COUNT(*) FROM rule_task WHERE device_id=1").Scan(&count); err != nil || count != 0 {
		t.Fatal("orphan task", count, err)
	}
}

func TestRuleTaskFutureSampleAndOffline(t *testing.T) {
	p, now := seed(t)
	ctx := context.Background()
	policy := Default()
	policy.Consecutive = 1
	payload, _ := json.Marshal(policy)
	if _, err := p.ExecContext(ctx, "INSERT INTO rule_policy(device_id,revision,payload,updated_by) VALUES(1,1,?,1)", json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExecContext(ctx, "UPDATE device SET last_time=? WHERE device_id=1", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	future := now.Add(10 * time.Second)
	save(t, p, health.Sample{RecordID: "future", Source: "watch", SourceDevice: "RULE001", Metric: "heart_rate", Unit: "count/min", Start: future, End: future, Version: 1, Value: health.Number(150)})
	if _, err := p.ExecContext(ctx, "UPDATE rule_task SET due_at=? WHERE device_id=1", now); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: p}
	if err := e.device(ctx, 1, now, &now, false); err != nil {
		t.Fatal(err)
	}
	var due time.Time
	if err := p.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&due); err != nil || !due.Equal(future) {
		t.Fatal("future observation not scheduled", due, err)
	}
	var count int
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident WHERE kind='rule_no_report'").Scan(&count); err != nil || count != 1 {
		t.Fatal("offline timer", count, err)
	}
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident WHERE kind='rule_heart_rate'").Scan(&count); err != nil || count != 0 {
		t.Fatal("premature incident", count, err)
	}
	// A separate worker/restarted process sees the same durable task.
	e2 := Engine{DB: p}
	if err := e2.device(ctx, 1, future, &future, false); err != nil {
		t.Fatal(err)
	}
	if err := e.device(ctx, 1, future, &future, false); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident WHERE kind='rule_heart_rate' AND severity='critical'").Scan(&count); err != nil || count != 1 {
		t.Fatal("future night incident missing/duplicated", count, err)
	}
}

func TestRuleTaskYieldsToLockedDevice(t *testing.T) {
	p, _ := seed(t)
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT device_id FROM device WHERE device_id=1 FOR UPDATE").Scan(&id); err != nil {
		t.Fatal(err)
	}
	var original time.Time
	if err = p.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&original); err != nil {
		t.Fatal(err)
	}
	tickCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err = (&Engine{DB: p}).Tick(tickCtx); err != nil {
		t.Fatal("worker waited on device", err)
	}
	var due time.Time
	if err = p.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&due); err != nil || !due.Equal(original) {
		t.Fatal("busy device lost or moved task", due, err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err = (&Engine{DB: p}).Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&due); err != nil || !due.After(original) {
		t.Fatal("released task not processed", due, err)
	}
}

func TestRuleTaskOfflineThresholdTransition(t *testing.T) {
	p, now := seed(t)
	ctx := context.Background()
	policy := Default()
	policy.OfflineSeconds = 75
	payload, _ := json.Marshal(policy)
	if _, err := p.ExecContext(ctx, "INSERT INTO rule_policy(device_id,revision,payload,updated_by) VALUES(1,1,?,1)", json.RawMessage(payload)); err != nil {
		t.Fatal(err)
	}
	// The timeout and preceding evaluation are in the same UTC minute.
	report := now.Add(-75*time.Second + 10*time.Second)
	boundary := report.Add(75 * time.Second)
	if _, err := p.ExecContext(ctx, "UPDATE device SET last_time=? WHERE device_id=1", report); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExecContext(ctx, "UPDATE rule_task SET due_at=? WHERE device_id=1", now); err != nil {
		t.Fatal(err)
	}
	e := Engine{DB: p}
	if err := e.device(ctx, 1, now, &now, false); err != nil {
		t.Fatal(err)
	}
	var due time.Time
	if err := p.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&due); err != nil || !due.Equal(boundary.Add(time.Microsecond)) {
		t.Fatal("wrong deadline", due, err)
	}
	if err := e.device(ctx, 1, boundary, &boundary, false); err != nil {
		t.Fatal(err)
	}
	due = boundary.Add(time.Microsecond)
	if err := (&Engine{DB: p}).device(ctx, 1, due, &due, false); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE kind='rule_no_report'").Scan(&count); err != nil || count != 1 {
		t.Fatal("same-minute threshold missed", count, err)
	}
	var next time.Time
	if err := p.QueryRowContext(ctx, "SELECT due_at FROM rule_task WHERE device_id=1").Scan(&next); err != nil || !next.Equal(nextNightBoundary(policy, due)) {
		t.Fatal("offline task continues minute polling", next, err)
	}
	if err := e.Device(ctx, 1, due.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE kind='rule_no_report'").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate offline incident", count, err)
	}
	// A fresh report rearms the exact timeout, without restarting the worker.
	recovered := due.Add(2 * time.Minute)
	if _, err := p.ExecContext(ctx, "UPDATE device SET last_time=? WHERE device_id=1", recovered); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExecContext(ctx, "UPDATE rule_task SET due_at=? WHERE device_id=1", recovered); err != nil {
		t.Fatal(err)
	}
	if err := e.device(ctx, 1, recovered, &recovered, false); err != nil {
		t.Fatal(err)
	}
	var active bool
	if err := p.QueryRowContext(ctx, "SELECT active FROM rule_state WHERE device_id=1 AND kind='no_report'").Scan(&active); err != nil || active {
		t.Fatal("recovery not persisted", active, err)
	}
	next = recovered.Add(75*time.Second + time.Microsecond)
	if err := e.device(ctx, 1, next, &next, false); err != nil {
		t.Fatal(err)
	}
	if err := p.QueryRowContext(ctx, "SELECT count(*) FROM care_incident WHERE kind='rule_no_report'").Scan(&count); err != nil || count != 2 {
		t.Fatal("next outage missing", count, err)
	}
}
