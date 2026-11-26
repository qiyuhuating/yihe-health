package rules

import (
	"math"
	"strconv"
	"time"
)

func referenceEvaluate(p Policy, in Input) []Candidate {
	if !p.Enabled {
		return nil
	}
	night := p.Night(in.Now)
	severity := "high"
	if night {
		severity = "critical"
	}
	capacity := 0
	for _, item := range []struct {
		kind  string
		value *Value
	}{{"heart_rate", in.Heart}, {"blood_pressure", in.Blood}} {
		if item.value != nil {
			capacity++
			if base := in.Baselines[item.kind]; base.Count >= p.BaselineMinimum && base.Mean > 0 {
				capacity++
			}
		}
	}
	if in.Battery != nil {
		capacity++
	}
	if !in.LastReport.IsZero() {
		capacity++
	}
	if in.Activity != nil && night {
		capacity++
	}
	out := make([]Candidate, 0, capacity)
	for _, m := range []struct {
		kind      string
		v         *Value
		low, high float64
	}{{"heart_rate", in.Heart, p.HRLow, p.HRHigh}, {"blood_pressure", in.Blood, p.BPLow, p.BPHigh}} {
		if m.v == nil || in.Now.Sub(m.v.At) > 5*time.Minute || m.v.At.After(in.Now.Add(time.Minute)) {
			continue
		}
		out = append(out, Candidate{m.kind, m.v.Key, severity, m.v.Value < m.low || m.v.Value > m.high, p.Consecutive, map[string]any{"value": m.v.Value, "low": m.low, "high": m.high, "source": m.v.Source, "sourceDevice": m.v.Device, "observedAt": m.v.At, "night": night}})
		base := in.Baselines[m.kind]
		if base.Count >= p.BaselineMinimum && base.Mean > 0 {
			delta := math.Abs(m.v.Value - base.Mean)
			limit := math.Max(math.Abs(base.Mean)*p.BaselinePercent/100, base.Deviation*p.BaselineSigma)
			// The public contract now includes metric-specific absolute floors.
			if m.kind == "blood_pressure" && limit < 12 {
				limit = 12
			} else if m.kind == "heart_rate" && limit < 8 {
				limit = 8
			}
			out = append(out, Candidate{"baseline_" + m.kind, m.v.Key, severity, delta > limit, p.Consecutive, map[string]any{"value": m.v.Value, "mean": base.Mean, "deviation": base.Deviation, "count": base.Count, "threshold": limit, "days": p.BaselineDays, "source": m.v.Source, "sourceDevice": m.v.Device, "observedAt": m.v.At}})
		}
	}
	if in.Battery != nil {
		out = append(out, Candidate{"battery", in.Battery.Key, "normal", in.Battery.Value < p.BatteryLow, 1, map[string]any{"value": in.Battery.Value, "threshold": p.BatteryLow}})
	}
	if !in.LastReport.IsZero() {
		key := strconv.FormatInt(in.LastReport.UnixMicro(), 10) + ":" + strconv.FormatInt(in.Now.UTC().Truncate(time.Minute).Unix(), 10)
		out = append(out, Candidate{"no_report", key, severity, in.Now.Sub(in.LastReport) > time.Duration(p.OfflineSeconds)*time.Second, 1, map[string]any{"lastReport": in.LastReport, "thresholdSeconds": p.OfflineSeconds}})
	}
	if in.Activity != nil && night && p.Night(in.Activity.At) {
		out = append(out, Candidate{"night_activity", in.Activity.Key, "high", in.Activity.Value >= p.NightSteps, 1, map[string]any{"steps": in.Activity.Value, "threshold": p.NightSteps, "windowMinutes": p.ActivityMinutes, "source": in.Activity.Source, "sourceDevice": in.Activity.Device}})
	}
	return out
}

type Candidate struct {
	Kind, Key, Severity string
	Breach              bool
	Required            int
	Evidence            map[string]any
}

func (e evidenceValue) asMap() map[string]any {
	switch e.kind {
	case evidenceThreshold:
		return map[string]any{"value": e.value, "low": e.low, "high": e.high, "source": e.source, "sourceDevice": e.device, "observedAt": e.observedAt, "night": e.night}
	case evidenceBaseline:
		return map[string]any{"value": e.value, "mean": e.mean, "deviation": e.deviation, "count": e.count, "threshold": e.threshold, "days": e.days, "source": e.source, "sourceDevice": e.device, "observedAt": e.observedAt}
	case evidenceBattery:
		return map[string]any{"value": e.value, "threshold": e.threshold}
	case evidenceOffline:
		return map[string]any{"lastReport": e.lastReport, "thresholdSeconds": e.offlineSeconds}
	case evidenceActivity:
		return map[string]any{"steps": e.value, "threshold": e.threshold, "windowMinutes": e.windowMinutes, "source": e.source, "sourceDevice": e.device}
	default:
		return nil
	}
}
