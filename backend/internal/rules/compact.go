package rules

import (
	"encoding/json"
	"math"
	"strconv"
	"time"
)

// CompactCandidate is an immutable snapshot of one rule result. Its evidence
// does not allocate a map. Values are copied, never retained via Input pointers.
type CompactCandidate struct {
	Kind, Key, Severity string
	Breach              bool
	Required            int
	evidence            evidenceValue
}

type evidenceKind uint8

const (
	evidenceThreshold evidenceKind = iota + 1
	evidenceBaseline
	evidenceBattery
	evidenceOffline
	evidenceActivity
)

type evidenceValue struct {
	kind                                         evidenceKind
	value, low, high, mean, deviation, threshold float64
	source, device                               string
	observedAt, lastReport                       time.Time
	count, days, windowMinutes, offlineSeconds   int
	night                                        bool
}

func (e evidenceValue) sourceValue() any {
	if e.kind == evidenceThreshold || e.kind == evidenceBaseline || e.kind == evidenceActivity {
		return e.source
	}
	return nil
}

type evidenceState struct {
	Breach   bool      `json:"breach"`
	Streak   int       `json:"streak"`
	Required int       `json:"required"`
	OwnerID  int64     `json:"ownerId"`
	At       time.Time `json:"at"`
}

// marshalEvidence retains the previous evidence JSON field names and types.
// Struct encoders are cached by encoding/json; no dynamic map is constructed.
func (c CompactCandidate) marshalEvidence(streak int, owner int64, now time.Time) ([]byte, error) {
	e := c.evidence
	state := evidenceState{c.Breach, streak, c.Required, owner, now}
	switch e.kind {
	case evidenceThreshold:
		return json.Marshal(struct {
			evidenceState
			Value      float64   `json:"value"`
			Low        float64   `json:"low"`
			High       float64   `json:"high"`
			Source     string    `json:"source"`
			Device     string    `json:"sourceDevice"`
			ObservedAt time.Time `json:"observedAt"`
			Night      bool      `json:"night"`
		}{state, e.value, e.low, e.high, e.source, e.device, e.observedAt, e.night})
	case evidenceBaseline:
		return json.Marshal(struct {
			evidenceState
			Value      float64   `json:"value"`
			Mean       float64   `json:"mean"`
			Deviation  float64   `json:"deviation"`
			Count      int       `json:"count"`
			Threshold  float64   `json:"threshold"`
			Days       int       `json:"days"`
			Source     string    `json:"source"`
			Device     string    `json:"sourceDevice"`
			ObservedAt time.Time `json:"observedAt"`
		}{state, e.value, e.mean, e.deviation, e.count, e.threshold, e.days, e.source, e.device, e.observedAt})
	case evidenceBattery:
		return json.Marshal(struct {
			evidenceState
			Value     float64 `json:"value"`
			Threshold float64 `json:"threshold"`
		}{state, e.value, e.threshold})
	case evidenceOffline:
		return json.Marshal(struct {
			evidenceState
			LastReport       time.Time `json:"lastReport"`
			ThresholdSeconds int       `json:"thresholdSeconds"`
		}{state, e.lastReport, e.offlineSeconds})
	case evidenceActivity:
		return json.Marshal(struct {
			evidenceState
			Steps         float64 `json:"steps"`
			Threshold     float64 `json:"threshold"`
			WindowMinutes int     `json:"windowMinutes"`
			Source        string  `json:"source"`
			Device        string  `json:"sourceDevice"`
		}{state, e.value, e.threshold, e.windowMinutes, e.source, e.device})
	default:
		return nil, nil
	}
}

// EvaluateCompactInto writes into caller-provided storage. At most seven
// candidates are emitted; callers may pass a [7]CompactCandidate on the stack.
// Results must not outlive or share a buffer that the caller reuses.
func EvaluateCompactInto(dst []CompactCandidate, p Policy, in Input) []CompactCandidate {
	if !p.Enabled {
		return dst[:0]
	}
	night := p.Night(in.Now)
	severity := "high"
	if night {
		severity = "critical"
	}
	result := dst[:0]
	for _, m := range [...]struct {
		kind, baselineKind string
		value              *Value
		low, high          float64
	}{
		{"heart_rate", "baseline_heart_rate", in.Heart, p.HRLow, p.HRHigh},
		{"blood_pressure", "baseline_blood_pressure", in.Blood, p.BPLow, p.BPHigh},
	} {
		v := m.value
		if v == nil || in.Now.Sub(v.At) > 5*time.Minute || v.At.After(in.Now.Add(time.Minute)) {
			continue
		}
		result = append(result, CompactCandidate{m.kind, v.Key, severity, v.Value < m.low || v.Value > m.high, p.Consecutive, evidenceValue{kind: evidenceThreshold, value: v.Value, low: m.low, high: m.high, source: v.Source, device: v.Device, observedAt: v.At, night: night}})
		base := in.Baselines[m.kind]
		if base.Count >= p.BaselineMinimum && base.Mean > 0 {
			delta := math.Abs(v.Value - base.Mean)
			limit := math.Max(math.Abs(base.Mean)*p.BaselinePercent/100, base.Deviation*p.BaselineSigma)
			floor := 8.0
			if m.kind == "blood_pressure" {
				floor = 12
			}
			limit = math.Max(limit, floor)
			result = append(result, CompactCandidate{m.baselineKind, v.Key, severity, delta > limit, p.Consecutive, evidenceValue{kind: evidenceBaseline, value: v.Value, mean: base.Mean, deviation: base.Deviation, count: base.Count, threshold: limit, days: p.BaselineDays, source: v.Source, device: v.Device, observedAt: v.At}})
		}
	}
	if v := in.Battery; v != nil {
		result = append(result, CompactCandidate{"battery", v.Key, "normal", v.Value < p.BatteryLow, 1, evidenceValue{kind: evidenceBattery, value: v.Value, threshold: p.BatteryLow}})
	}
	if !in.LastReport.IsZero() {
		var keyBuffer [48]byte
		keyBytes := strconv.AppendInt(keyBuffer[:0], in.LastReport.UnixMicro(), 10)
		keyBytes = append(keyBytes, ':')
		keyBytes = strconv.AppendInt(keyBytes, in.Now.UTC().Truncate(time.Minute).Unix(), 10)
		key := string(keyBytes)
		result = append(result, CompactCandidate{"no_report", key, severity, in.Now.Sub(in.LastReport) > time.Duration(p.OfflineSeconds)*time.Second, 1, evidenceValue{kind: evidenceOffline, lastReport: in.LastReport, offlineSeconds: p.OfflineSeconds}})
	}
	if v := in.Activity; v != nil && night && p.Night(v.At) {
		result = append(result, CompactCandidate{"night_activity", v.Key, "high", v.Value >= p.NightSteps, 1, evidenceValue{kind: evidenceActivity, value: v.Value, threshold: p.NightSteps, windowMinutes: p.ActivityMinutes, source: v.Source, device: v.Device}})
	}
	return result
}
