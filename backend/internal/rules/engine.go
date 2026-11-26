package rules

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"yihe-care/internal/db"
	"yihe-care/internal/health"
	"yihe-care/internal/observability"
	"yihe-care/internal/securestore"
)

const latestSampleTail = ` FROM health_sample WHERE old_id=? AND device_id=? AND epoch=? AND metric=? AND NOT deleted AND sample_start>=? AND sample_start<=? ORDER BY sample_start DESC,revision DESC LIMIT 1`
const latestSampleSQL = `SELECT LOWER(HEX(sample_id)),revision,payload,` + health.SampleCipherSQL + " AS payload_cipher" + latestSampleTail

// Each branch retains its own index range and LIMIT. The explicit ordinal
// preserves input/decode order and avoids a shared window across metrics.
const latestRuleObservationsSQL = `SELECT metric_order,LOWER(HEX(sample_id)),revision,payload,payload_cipher FROM (
 (SELECT 0 AS metric_order,sample_id,revision,payload,` + health.SampleCipherSQL + " AS payload_cipher" + latestSampleTail + `)
 UNION ALL (SELECT 1 AS metric_order,sample_id,revision,payload,` + health.SampleCipherSQL + " AS payload_cipher" + latestSampleTail + `)
 UNION ALL (SELECT 2 AS metric_order,sample_id,revision,payload,` + health.SampleCipherSQL + " AS payload_cipher" + latestSampleTail + `)
 ) AS latest ORDER BY metric_order`

const baselineCoverSQL = `SELECT count(*) AS sample_count,count(rule_value) AS value_count,COALESCE(avg(rule_value),0) AS mean,COALESCE(stddev_pop(rule_value),0) AS deviation
 FROM health_sample FORCE INDEX (idx_health_rule_cover)
 WHERE old_id=? AND device_id=? AND metric=? AND deleted=false AND payload_encrypted=false
 AND source=? AND rule_identity_key=? AND BINARY source=BINARY ?
 AND sample_start>=? AND sample_start<? AND rule_legacy_unzoned=false`

type Engine struct {
	DB  *db.Database
	Box *securestore.Box
	// Workers is configurable to keep rule transactions from monopolizing the
	// shared SQL pool during health ingest bursts; 0 preserves the old 8-worker default.
	Workers int
}

func (e *Engine) Device(ctx context.Context, id int64, now time.Time) error {
	return e.device(ctx, id, now, nil, false)
}

func (e *Engine) device(ctx context.Context, id int64, now time.Time, cutoff *time.Time, skipBusy bool) error {
	tx, err := e.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	device := ruleDevice{id: id}
	query := "SELECT old_id,health_epoch,device_no,last_time,bind_time,battery FROM device WHERE device_id=? AND old_id IS NOT NULL AND status<>0 FOR UPDATE"
	if skipBusy {
		// Background work yields to ingest/SOS that already owns the device.
		// The unchanged durable due row remains eligible for the next tick.
		query += " SKIP LOCKED"
	}
	err = tx.QueryRowContext(ctx, query, id).Scan(&device.oldID, &device.epoch, &device.no, &device.last, &device.bound, &device.battery)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Candidate selection never holds task locks. Always acquire the device
	// row first, then its task, matching the write-side device trigger.
	if cutoff != nil {
		var epoch int64
		err = tx.QueryRowContext(ctx, "SELECT epoch FROM rule_task WHERE device_id=? AND eligible AND due_at<=? FOR UPDATE", id, *cutoff).Scan(&epoch)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if epoch != device.epoch {
			return errors.New("rule task binding epoch mismatch")
		}
	}
	observability.Default.Add("yihe_rule_device_evaluations_total", 1)
	p, err := readRulePolicy(ctx, tx, id)
	if err != nil {
		return err
	}
	if !p.Enabled {
		if cutoff != nil {
			if err := scheduleRuleTask(ctx, tx, device, nil); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	in, err := collectRuleInput(ctx, tx, device, p, now, e.Box)
	if err != nil {
		return err
	}
	var candidateBuffer [7]CompactCandidate
	// Evidence is consumed by the synchronous Exec before this buffer is reused.
	// A device owns its buffer: no global pool of private health observations.
	var evidenceBuffer [512]byte
	for _, candidate := range EvaluateCompactInto(candidateBuffer[:0], p, in) {
		if err = e.persist(ctx, tx, id, device.oldID, device.epoch, candidate, now, evidenceBuffer[:0]); err != nil {
			return err
		}
	}
	nightIncident, err := upgradeNightIncident(ctx, tx, device, p, now)
	if err != nil {
		return err
	}
	if cutoff != nil {
		due, err := nextRuleTask(ctx, tx, device, p, in, nightIncident)
		if err != nil {
			return err
		}
		if err := scheduleRuleTask(ctx, tx, device, due); err != nil {
			return err
		}
	}
	return tx.Commit()
}
