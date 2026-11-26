package rules

import (
	"math"
	"strconv"
	"time"

	"yihe-care/internal/jsonscan"
)

// appendEvidenceFast reuses caller-owned bytes for the common evidence format.
// The returned slice aliases dst; consumers must finish using it before
// reusing dst. Fallbacks return independent encoding/json allocations.
// Never put this borrowed buffer in an asynchronous queue or retain it.
func (c CompactCandidate) appendEvidenceFast(dst []byte, streak int, owner int64, at time.Time) ([]byte, error) {
	e := c.evidence
	if !e.canUseFastJSON(at) {
		return c.marshalEvidence(streak, owner, at)
	}
	capacity := 192
	if e.kind == evidenceThreshold || e.kind == evidenceBaseline {
		capacity = 256
	}
	b := dst[:0]
	if cap(b) == 0 {
		b = make([]byte, 0, capacity)
	}
	b = append(b, `{"breach":`...)
	b = strconv.AppendBool(b, c.Breach)
	b = append(b, `,"streak":`...)
	b = strconv.AppendInt(b, int64(streak), 10)
	b = append(b, `,"required":`...)
	b = strconv.AppendInt(b, int64(c.Required), 10)
	b = append(b, `,"ownerId":`...)
	b = strconv.AppendInt(b, owner, 10)
	b = append(b, `,"at":"`...)
	b = at.AppendFormat(b, time.RFC3339Nano)
	b = append(b, '"')
	switch e.kind {
	case evidenceThreshold:
		b = append(b, `,"value":`...)
		b = appendFloatEvidence(b, e.value)
		b = append(b, `,"low":`...)
		b = appendFloatEvidence(b, e.low)
		b = append(b, `,"high":`...)
		b = appendFloatEvidence(b, e.high)
		b = append(b, `,"source":"`...)
		b = append(b, e.source...)
		b = append(b, '"')
		b = append(b, `,"sourceDevice":"`...)
		b = append(b, e.device...)
		b = append(b, '"')
		b = append(b, `,"observedAt":"`...)
		b = e.observedAt.AppendFormat(b, time.RFC3339Nano)
		b = append(b, '"')
		b = append(b, `,"night":`...)
		b = strconv.AppendBool(b, e.night)
	case evidenceBaseline:
		b = append(b, `,"value":`...)
		b = appendFloatEvidence(b, e.value)
		b = append(b, `,"mean":`...)
		b = appendFloatEvidence(b, e.mean)
		b = append(b, `,"deviation":`...)
		b = appendFloatEvidence(b, e.deviation)
		b = append(b, `,"count":`...)
		b = strconv.AppendInt(b, int64(e.count), 10)
		b = append(b, `,"threshold":`...)
		b = appendFloatEvidence(b, e.threshold)
		b = append(b, `,"days":`...)
		b = strconv.AppendInt(b, int64(e.days), 10)
		b = append(b, `,"source":"`...)
		b = append(b, e.source...)
		b = append(b, '"')
		b = append(b, `,"sourceDevice":"`...)
		b = append(b, e.device...)
		b = append(b, '"')
		b = append(b, `,"observedAt":"`...)
		b = e.observedAt.AppendFormat(b, time.RFC3339Nano)
		b = append(b, '"')
	case evidenceBattery:
		b = append(b, `,"value":`...)
		b = appendFloatEvidence(b, e.value)
		b = append(b, `,"threshold":`...)
		b = appendFloatEvidence(b, e.threshold)
	case evidenceOffline:
		b = append(b, `,"lastReport":"`...)
		b = e.lastReport.AppendFormat(b, time.RFC3339Nano)
		b = append(b, '"')
		b = append(b, `,"thresholdSeconds":`...)
		b = strconv.AppendInt(b, int64(e.offlineSeconds), 10)
	case evidenceActivity:
		b = append(b, `,"steps":`...)
		b = appendFloatEvidence(b, e.value)
		b = append(b, `,"threshold":`...)
		b = appendFloatEvidence(b, e.threshold)
		b = append(b, `,"windowMinutes":`...)
		b = strconv.AppendInt(b, int64(e.windowMinutes), 10)
		b = append(b, `,"source":"`...)
		b = append(b, e.source...)
		b = append(b, '"')
		b = append(b, `,"sourceDevice":"`...)
		b = append(b, e.device...)
		b = append(b, '"')
	default:
		return c.marshalEvidence(streak, owner, at)
	}
	return append(b, '}'), nil
}

func (e *evidenceValue) canUseFastJSON(at time.Time) bool {
	if !fastJSONTime(at) {
		return false
	}
	switch e.kind {
	case evidenceThreshold:
		if !fastJSONTime(e.observedAt) || !fastJSONString(e.source) || !fastJSONString(e.device) ||
			!finite(e.value) || !finite(e.low) || !finite(e.high) {
			return false
		}
	case evidenceBaseline:
		if !fastJSONTime(e.observedAt) || !fastJSONString(e.source) || !fastJSONString(e.device) ||
			!finite(e.value) || !finite(e.mean) || !finite(e.deviation) || !finite(e.threshold) {
			return false
		}
	case evidenceBattery:
		if !finite(e.value) || !finite(e.threshold) {
			return false
		}
	case evidenceOffline:
		if !fastJSONTime(e.lastReport) {
			return false
		}
	case evidenceActivity:
		if !fastJSONString(e.source) || !fastJSONString(e.device) || !finite(e.value) || !finite(e.threshold) {
			return false
		}
	default:
		return false
	}

	return true
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func fastJSONTime(t time.Time) bool {
	// Time.MarshalJSON rejects years out of range; offsets not in full minutes
	// are unusual and delegated to its own RFC3339 encoder.
	y := t.Year()
	_, offset := t.Zone()
	return y >= 0 && y <= 9999 && offset%60 == 0 && offset > -24*3600 && offset < 24*3600
}
func fastJSONString(s string) bool { return jsonscan.Safe(s) }
func appendFloatEvidence(b []byte, value float64) []byte {
	// Physiological observations and configured limits commonly are integers.
	// Preserve negative zero and scientific-notation thresholds exactly.
	if value != 0 && value > -1e15 && value < 1e15 && value == math.Trunc(value) {
		return strconv.AppendInt(b, int64(value), 10)
	}
	f := byte('f')
	abs := math.Abs(value)
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		f = 'e'
	}
	b = strconv.AppendFloat(b, value, f, -1, 64)
	if f == 'e' {
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}
