//go:build yihe_next_gateway

package rules

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

// The bounded escape hatch recomputes from current SQL state in the
// collector process. No IPC occurs while it owns locks. Do not queue behind
// SOS/ingest; an unavailable fence leaves the exact durable task unchanged.
func recomputeNativeRule(ctx context.Context, database *db.Database, box *securestore.Box, req NativeCollectRequest) (reply NativeCollectReply, err error) {
	reply.Challenge = req.Challenge
	if req.Device <= 0 || len(req.Challenge) != 32 || req.DueUS <= 0 || req.NowNS/1000-req.DueUS < 2_000_000 {
		return reply, errors.New("invalid recompute request")
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	defer func() {
		var mysqlErr *mysql.MySQLError
		if err != nil && (errors.Is(err, sql.ErrNoRows) || ctx.Err() != nil || errors.As(err, &mysqlErr) && (mysqlErr.Number == 3572 || mysqlErr.Number == 1213 || mysqlErr.Number == 1205)) {
			reply.Stale = true
			err = nil
		}
	}()
	now := time.Unix(0, req.NowNS).UTC()
	due := time.UnixMicro(req.DueUS).UTC()
	var owner int64
	if err = database.QueryRowContext(ctx, "SELECT old_id FROM device WHERE device_id=? AND old_id IS NOT NULL AND status<>0", req.Device).Scan(&owner); err != nil {
		return
	}
	tx, e := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return reply, e
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT old_id FROM oldperson WHERE old_id=? FOR UPDATE NOWAIT", owner).Scan(&id); err != nil {
		return
	}
	device := ruleDevice{id: req.Device}
	if err = tx.QueryRowContext(ctx, "SELECT old_id,health_epoch,device_no,last_time,bind_time,battery FROM device WHERE device_id=? AND status<>0 AND old_id IS NOT NULL FOR UPDATE NOWAIT", req.Device).Scan(&device.oldID, &device.epoch, &device.no, &device.last, &device.bound, &device.battery); err != nil {
		return
	}
	if device.oldID != owner {
		reply.Stale = true
		return
	}
	for _, q := range []string{"SELECT device_id FROM rule_policy WHERE device_id=? FOR UPDATE NOWAIT", "SELECT device_id FROM watch_info WHERE device_id=? FOR UPDATE NOWAIT"} {
		if e = tx.QueryRowContext(ctx, q, req.Device).Scan(&id); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return reply, e
		}
	}
	var epoch int64
	if err = tx.QueryRowContext(ctx, "SELECT epoch FROM rule_task WHERE device_id=? AND eligible AND due_at=? FOR UPDATE NOWAIT", req.Device, due).Scan(&epoch); err != nil {
		return
	}
	if epoch != device.epoch {
		reply.Stale = true
		return
	}
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM health_owner_clock WHERE old_id=? FOR UPDATE NOWAIT", owner).Scan(&id); err != nil {
		return
	}
	p, e := readRulePolicy(ctx, tx, req.Device)
	if e != nil {
		return reply, e
	}
	var next any
	if p.Enabled {
		in, e := collectRuleInput(ctx, tx, device, p, now, box)
		if e != nil {
			return reply, e
		}
		engine := Engine{DB: database, Box: box}
		var scratch [7]CompactCandidate
		var evidence [512]byte
		for _, c := range EvaluateCompactInto(scratch[:0], p, in) {
			if err = engine.persist(ctx, tx, device.id, owner, epoch, c, now, evidence[:0]); err != nil {
				return
			}
		}
		night, e := upgradeNightIncident(ctx, tx, device, p, now)
		if e != nil {
			return reply, e
		}
		at, e := nextRuleTask(ctx, tx, device, p, in, night)
		if e != nil {
			return reply, e
		}
		next = at
	}
	digest := box.Index("yihe-next-rule-recompute-v1", req.Challenge)
	if _, err = tx.ExecContext(ctx, "INSERT INTO rust_rule_receipt(device_id,epoch,request_id,payload_digest) VALUES(?,?,?,?)", req.Device, epoch, "recompute-"+req.Challenge, digest); err != nil {
		return
	}
	result, e := tx.ExecContext(ctx, "UPDATE rule_task SET due_at=? WHERE device_id=? AND epoch=? AND eligible AND due_at=?", next, req.Device, epoch, due)
	if e != nil {
		return reply, e
	}
	changed, e := result.RowsAffected()
	if e != nil {
		return reply, e
	}
	if changed != 1 {
		reply.Stale = true
		return
	}
	if err = tx.Commit(); err != nil {
		return
	}
	reply.Recomputed = true
	return
}
