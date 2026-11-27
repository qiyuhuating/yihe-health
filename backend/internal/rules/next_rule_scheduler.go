//go:build yihe_next_gateway

package rules

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

//go:embed native_rule_fence.sql
var nativeRuleFenceSQL string

//go:embed native_rule_health_delta.sql
var nativeRuleHealthDeltaSQL string

// NativeCollectRequest is an internal pipe message from the Rust scheduler.
// No writer locks or SQL connection are held while the collector responds.
type NativeCollectRequest struct {
	State          string `json:"state"`
	Device         int64  `json:"device"`
	Owner          int64  `json:"owner"`
	Epoch          int64  `json:"epoch"`
	NowNS          int64  `json:"now_ns"`
	OwnerRevision  int64  `json:"owner_revision"`
	PolicyRevision *int64 `json:"policy_revision"`
	Challenge      string `json:"challenge"`
	Fence          string `json:"fence"`
	DueUS          int64  `json:"due_us"`
}

type nativeObservation struct {
	Value float64 `json:"value"`
	AtNS  int64   `json:"at_ns"`
}
type nativeBaseline struct {
	Count     int     `json:"count"`
	Mean      float64 `json:"mean"`
	Deviation float64 `json:"deviation"`
}
type nativeRuleInput struct {
	Enabled       bool               `json:"enabled"`
	Night         bool               `json:"night"`
	ActivityNight bool               `json:"activity_night"`
	NowNS         int64              `json:"now_ns"`
	LastReportNS  *int64             `json:"last_report_ns"`
	Heart         *nativeObservation `json:"heart"`
	Blood         *nativeObservation `json:"blood"`
	Battery       *nativeObservation `json:"battery"`
	Activity      *nativeObservation `json:"activity"`
	HeartBaseline *nativeBaseline    `json:"heart_baseline"`
	BloodBaseline *nativeBaseline    `json:"blood_baseline"`
}
type nativeEvidence struct {
	Kind string          `json:"kind"`
	Key  string          `json:"key"`
	Data json.RawMessage `json:"data"`
}
type nativeBatch struct {
	Device    int64            `json:"device"`
	Owner     int64            `json:"owner"`
	Epoch     int64            `json:"epoch"`
	RequestID string           `json:"request_id"`
	Input     nativeRuleInput  `json:"input"`
	Evidence  []nativeEvidence `json:"evidence"`
}
type NativeCollectReply struct {
	Challenge      string       `json:"challenge"`
	Fallback       bool         `json:"fallback"`
	Stale          bool         `json:"stale"`
	Recomputed     bool         `json:"recomputed"`
	Fence          string       `json:"fence"`
	OwnerRevision  *int64       `json:"owner_revision"`
	PolicyRevision *int64       `json:"policy_revision"`
	Policy         *Policy      `json:"policy"`
	NextDueNS      *int64       `json:"next_due_ns"`
	Batch          *nativeBatch `json:"batch"`
}

// CollectNativeRuleInput preserves the actual Go baseline, legacy vital,
// activity and timezone algorithms in a nonlocking consistent snapshot.
// It is a trusted local adapter, not a public API and not a task claimer.
// The collector validates its own snapshot fingerprint; Rust rechecks it
// under the write fences before committing. A stale result is retryable.
func CollectNativeRuleInput(ctx context.Context, database *db.Database, box *securestore.Box, req NativeCollectRequest) (NativeCollectReply, error) {
	if req.State == "recompute" {
		return recomputeNativeRule(ctx, database, box, req)
	}
	reply := NativeCollectReply{Challenge: req.Challenge}
	if req.State != "collect" || req.Device <= 0 || req.Owner <= 0 || req.Epoch <= 0 || len(req.Challenge) != 32 || len(req.Fence) != 64 {
		return reply, errors.New("invalid native collect request")
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return reply, err
	}
	defer tx.Rollback()
	now := time.Unix(0, req.NowNS).UTC()
	if err = tx.QueryRowContext(ctx, nativeRuleFenceSQL, now.Add(-5*time.Minute), req.Device).Scan(&reply.Fence); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			reply.Stale = true
			return reply, tx.Commit()
		}
		return reply, err
	}
	if reply.Fence != req.Fence {
		reply.Stale = true
		return reply, tx.Commit()
	}
	device := ruleDevice{id: req.Device}
	if err = tx.QueryRowContext(ctx, "SELECT old_id,health_epoch,device_no,last_time,bind_time,battery FROM device WHERE device_id=? AND old_id IS NOT NULL AND status<>0", req.Device).Scan(&device.oldID, &device.epoch, &device.no, &device.last, &device.bound, &device.battery); err != nil {
		return reply, err
	}
	if device.oldID != req.Owner || device.epoch != req.Epoch {
		return reply, errors.New("native collect binding changed")
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM health_owner_clock WHERE old_id=?", req.Owner).Scan(&revision); err != nil {
		return reply, fmt.Errorf("native owner revision unavailable: %w", err)
	}
	if revision != req.OwnerRevision {
		var safe bool
		if revision > req.OwnerRevision {
			if err = tx.QueryRowContext(ctx, nativeRuleHealthDeltaSQL, revision, req.Device, req.Owner, req.OwnerRevision, revision).Scan(&safe); err != nil {
				return reply, err
			}
		}
		if !safe {
			reply.Stale = true
			return reply, tx.Commit()
		}
	}
	// Return the original validated revision. Rust checks the entire delta
	// from that revision again while holding the owner clock at commit.
	revision = req.OwnerRevision
	var policyRevision *int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM rule_policy WHERE device_id=?", req.Device).Scan(&policyRevision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return reply, err
	}
	if (policyRevision == nil) != (req.PolicyRevision == nil) || policyRevision != nil && *policyRevision != *req.PolicyRevision {
		return reply, errors.New("native policy revision mismatch")
	}
	p, err := readRulePolicy(ctx, tx, req.Device)
	if err != nil {
		return reply, err
	}
	reply.OwnerRevision = &revision
	reply.PolicyRevision = policyRevision
	reply.Policy = &p
	batch := &nativeBatch{Device: req.Device, Owner: req.Owner, Epoch: req.Epoch, RequestID: "scheduler", Input: nativeRuleInput{Enabled: p.Enabled, NowNS: req.NowNS}, Evidence: make([]nativeEvidence, 0, 7)}
	if p.Enabled {
		// Night SOS/fence upgrade is not yet migrated. Never consume its task
		// or suppress the production Go escalation.
		if p.Night(now) {
			var active bool
			if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM care_incident WHERE device_id=? AND old_id=? AND kind IN ('sos','fence_exit') AND state<>'verified')", req.Device, req.Owner).Scan(&active); err != nil {
				return reply, err
			}
			if active {
				reply.Fallback = true
				return reply, tx.Commit()
			}
		}
		in, err := collectRuleInput(ctx, tx, device, p, now, box)
		if err != nil {
			return reply, err
		}
		obs := func(v *Value) *nativeObservation {
			if v == nil {
				return nil
			}
			return &nativeObservation{v.Value, v.At.UnixNano()}
		}
		base := func(kind string) *nativeBaseline {
			b, ok := in.Baselines[kind]
			if !ok {
				return nil
			}
			return &nativeBaseline{b.Count, b.Mean, b.Deviation}
		}
		batch.Input = nativeRuleInput{Enabled: true, Night: p.Night(now), ActivityNight: in.Activity != nil && p.Night(in.Activity.At), NowNS: req.NowNS, Heart: obs(in.Heart), Blood: obs(in.Blood), Battery: obs(in.Battery), Activity: obs(in.Activity), HeartBaseline: base("heart_rate"), BloodBaseline: base("blood_pressure")}
		if !in.LastReport.IsZero() {
			v := in.LastReport.UnixNano()
			batch.Input.LastReportNS = &v
		}
		var scratch [7]CompactCandidate
		for _, c := range EvaluateCompactInto(scratch[:0], p, in) {
			evidence, err := c.marshalEvidence(0, device.oldID, now)
			if err != nil {
				return reply, err
			}
			batch.Evidence = append(batch.Evidence, nativeEvidence{c.Kind, c.Key, evidence})
		}
		due, err := nextRuleTask(ctx, tx, device, p, in, false)
		if err != nil {
			return reply, err
		}
		ns := due.UnixNano()
		reply.NextDueNS = &ns
	}
	reply.Batch = batch
	return reply, tx.Commit()
}
