package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"time"
	"yihe-care/internal/health"
	"yihe-care/internal/securestore"
	"yihe-care/internal/vitalrepo"
)

type ruleDevice struct {
	id, oldID, epoch int64
	no               string
	last, bound      *time.Time
	battery          *float64
}

func readRulePolicy(ctx context.Context, tx *sql.Tx, id int64) (Policy, error) {
	var err error
	p := Default()
	var payload []byte
	err = tx.QueryRowContext(ctx, "SELECT payload FROM rule_policy WHERE device_id=?", id).Scan(&payload)
	if err == nil {
		if err = json.Unmarshal(payload, &p); err != nil {
			return Policy{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		var hrf, hrl, bpf, bpl *float64
		err = tx.QueryRowContext(ctx, "SELECT hrf,hrl,bpf,bpl FROM watch_info WHERE device_id=?", id).Scan(&hrf, &hrl, &bpf, &bpl)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Policy{}, err
		}
		if hrf != nil && hrl != nil {
			p.HRLow, p.HRHigh = min(*hrf, *hrl), max(*hrf, *hrl)
		}
		if bpf != nil && bpl != nil {
			p.BPLow, p.BPHigh = min(*bpf, *bpl), max(*bpf, *bpl)
		}
	} else {
		return Policy{}, err
	}
	if err = p.Validate(); err != nil {
		return Policy{}, err
	}
	return p, nil

}

func collectRuleInput(ctx context.Context, tx *sql.Tx, device ruleDevice, p Policy, now time.Time, box *securestore.Box) (Input, error) {
	last, bound, battery := device.last, device.bound, device.battery
	in := Input{Now: now, Baselines: map[string]Baseline{}}
	if last != nil {
		in.LastReport = *last
	} else if bound != nil {
		in.LastReport = *bound
	}
	if battery != nil && last != nil {
		in.Battery = &Value{Value: *battery, Key: last.Format(time.RFC3339Nano), At: *last}
	}
	if err := readLatestRuleObservations(ctx, tx, device, p, &in, box); err != nil {
		return Input{}, err
	}
	if err := readLegacyRuleVitals(ctx, tx, device, &in, box); err != nil {
		return Input{}, err
	}
	if err := readRuleBaselines(ctx, tx, device, p, &in, box); err != nil {
		return Input{}, err
	}
	if err := readRuleActivity(ctx, tx, device, p, &in, box); err != nil {
		return Input{}, err
	}
	return in, nil

}

func readLatestRuleObservations(ctx context.Context, tx *sql.Tx, device ruleDevice, p Policy, in *Input, box *securestore.Box) error {
	id, oldID, epoch := device.id, device.oldID, device.epoch
	now := in.Now
	var err error
	// Fetch one latest observation per metric so a heart-rate burst cannot hide
	// activity or blood pressure in a shared LIMIT window.
	for _, metric := range []string{"heart_rate", "blood_pressure_systolic", "steps"} {
		window := 5 * time.Minute
		if metric == "steps" {
			window = time.Duration(p.ActivityMinutes) * time.Minute
		}
		var sampleID string
		var revision int64
		var stored health.ProjectionPayload
		err = tx.QueryRowContext(ctx, latestSampleSQL, oldID, id, epoch, metric, now.Add(-window).UnixMicro(), now.UnixMicro()).
			Scan(&sampleID, &revision, &stored.JSON, &stored.Cipher)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		payload, err := stored.OpenSample(box, sampleID, oldID, revision)
		if err != nil {
			return err
		}
		var sample health.Sample
		if err = json.Unmarshal(payload, &sample); err != nil {
			return err
		}
		if sample.Value == nil || sample.TimeBasis == "legacy-unzoned" {
			continue
		}
		v := &Value{Value: *sample.Value, Key: sampleID + ":" + fmt.Sprint(revision), Source: sample.Source, Device: sample.SourceDevice, At: sample.Start}
		switch metric {
		case "heart_rate":
			in.Heart = v
		case "blood_pressure_systolic":
			in.Blood = v
		case "steps":
			if p.Night(now) {
				in.Activity = v
			}
		}
	}
	return nil

}

func readLegacyRuleVitals(ctx context.Context, tx *sql.Tx, device ruleDevice, in *Input, box *securestore.Box) error {
	id, oldID, no := device.id, device.oldID, device.no
	now := in.Now
	var err error
	if in.Heart == nil || in.Blood == nil {
		var record int64
		var at time.Time
		var stored vitalrepo.StoredMeasurements
		err = tx.QueryRowContext(ctx, "SELECT id,create_time,hr,bp,measurement_cipher FROM ratepre WHERE device_id=? AND old_id=? AND create_time>=? ORDER BY create_time DESC,id DESC LIMIT 1", id, oldID, now.Add(-5*time.Minute)).
			Scan(&record, &at, &stored.HeartRate, &stored.Pressure, &stored.Cipher)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			values, err := stored.Open(box, record, &oldID, &id)
			if err != nil {
				return err
			}
			if values.HeartRate != nil && in.Heart == nil {
				in.Heart = &Value{float64(*values.HeartRate), fmt.Sprint("legacy:", record), "legacy", no, at}
			}
			if values.Pressure != nil && in.Blood == nil {
				in.Blood = &Value{float64(*values.Pressure), fmt.Sprint("legacy:", record), "legacy", no, at}
			}
		}
	}
	return nil

}

func readRuleBaselines(ctx context.Context, tx *sql.Tx, device ruleDevice, p Policy, in *Input, box *securestore.Box) error {
	for _, item := range []struct {
		kind, metric string
		v            *Value
	}{{"heart_rate", "heart_rate", in.Heart}, {"blood_pressure", "blood_pressure_systolic", in.Blood}} {
		if item.v == nil || item.v.Source == "legacy" {
			continue
		}
		var previous string
		err := tx.QueryRowContext(ctx, "SELECT observation_key FROM rule_state WHERE device_id=? AND epoch=? AND kind=?",
			device.id, device.epoch, "baseline_"+item.kind).Scan(&previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Device and policy writes take the same device lock. Once this
		// version has a baseline state, persistence ignores its replays.
		// A missing state still allows late history to meet the minimum.
		if err == nil && previous == item.v.Key {
			continue
		}
		base, err := readRuleBaseline(ctx, tx, device, item.metric, *item.v, in.Now, p.BaselineDays, box)
		if err != nil {
			return err
		}
		in.Baselines[item.kind] = base
	}
	return nil

}

func readRuleActivity(ctx context.Context, tx *sql.Tx, device ruleDevice, p Policy, in *Input, box *securestore.Box) error {
	id, oldID, epoch := device.id, device.oldID, device.epoch
	now := in.Now
	if in.Activity != nil {
		v := in.Activity
		start := now.Add(-time.Duration(p.ActivityMinutes) * time.Minute)
		rows, err := tx.QueryContext(ctx, `SELECT sample_id,revision,payload,payload_cipher FROM health_sample WHERE old_id=? AND device_id=? AND epoch=? AND metric='steps' AND NOT deleted AND BINARY source=BINARY ? AND BINARY source_device=BINARY ? AND sample_start<=? AND (sample_start>=? OR sample_end>?) ORDER BY sample_start,revision LIMIT 1001`, oldID, id, epoch, v.Source, v.Device, now.UnixMicro(), start.UnixMicro(), start.UnixMicro())
		if err != nil {
			return err
		}
		var window activityWindow
		for rows.Next() {
			var sampleID string
			var revision int64
			var stored health.ProjectionPayload
			var sample health.Sample
			if err = rows.Scan(&sampleID, &revision, &stored.JSON, &stored.Cipher); err != nil {
				break
			}
			var payload []byte
			if payload, err = stored.OpenSample(box, sampleID, oldID, revision); err != nil {
				break
			}
			if err = json.Unmarshal(payload, &sample); err != nil {
				break
			}
			window.add(p, sample, start, now)
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		if value, ok := window.value(); !ok {
			in.Activity = nil
		} else {
			v.Value = value
		}
	}

	return nil

}
