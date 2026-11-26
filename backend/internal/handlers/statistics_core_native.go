//go:build yihecore && cgo && !purego && ((windows && amd64) || (linux && (amd64 || arm64)))

package handlers

/*
#cgo windows LDFLAGS: ${SRCDIR}/../../../yihe-core/target/x86_64-pc-windows-gnu/release/libyihe_core.a -lws2_32 -luserenv -lbcrypt -lntdll -ldbghelp -lsynchronization
#cgo linux LDFLAGS: ${SRCDIR}/../../../yihe-core/target/release/libyihe_core.a -ldl -lpthread -lm
#include <stdint.h>
#include <stddef.h>
typedef struct { uint64_t count; double min, max, mean; } yihe_result;
typedef struct { uint32_t sample_days, trusted_days, rejected_days, reason, confidence; double baseline, scale, score; } yihe_baseline;
int32_t yihe_core_baseline_v1(double, const uint8_t *, const double *, size_t, yihe_baseline *);
int32_t yihe_core_query_v1(const int64_t *, const double *, size_t, int64_t, int64_t, uint8_t, yihe_result *);
*/
import "C"

import (
	"math"
	"time"
	"unsafe"

	"yihe-care/internal/health"
	"yihe-care/internal/healthbaseline"
)

const coreEnabled = true

func coreBaseline(current healthbaseline.Observation, history []healthbaseline.Observation) (healthbaseline.Result, bool) {
	if len(history) < 128 || len(history) > 5000 || current.OwnerID <= 0 ||
		current.BindingVersion <= 0 || current.Metric != "heart_rate" || current.At.IsZero() ||
		!current.Valid || !(current.Value > 0 && current.Value <= 1e6) {
		return healthbaseline.Result{}, false
	}
	day := current.At.UTC().Truncate(24 * time.Hour)
	firstDay := day.AddDate(0, 0, -healthbaseline.HistoryDays)
	days := make([]uint8, 0, len(history))
	values := make([]float64, 0, len(history))
	for _, s := range history {
		if s.OwnerID != current.OwnerID || s.DeviceID != current.DeviceID ||
			s.BindingVersion != current.BindingVersion || s.Source != current.Source ||
			s.SourceDevice != current.SourceDevice || s.Metric != current.Metric ||
			!s.Valid || s.At.IsZero() || math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			continue
		}
		at := s.At.UTC().Truncate(24 * time.Hour)
		if at.Before(firstDay) || !at.Before(day) {
			continue
		}
		if !(s.Value > 0 && s.Value <= 1e6) {
			return healthbaseline.Result{}, false
		}
		days = append(days, uint8(at.Sub(firstDay)/(24*time.Hour)))
		values = append(values, s.Value)
	}
	if len(values) < 128 {
		return healthbaseline.Result{}, false
	}
	var raw C.yihe_baseline
	if C.yihe_core_baseline_v1(C.double(current.Value), (*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(days))),
		(*C.double)(unsafe.Pointer(unsafe.SliceData(values))), C.size_t(len(values)), &raw) != 1 {
		return healthbaseline.Result{}, false
	}
	result := healthbaseline.Result{SampleDays: int(raw.sample_days), TrustedDays: int(raw.trusted_days), RejectedDays: int(raw.rejected_days), Confidence: "insufficient"}
	switch raw.reason {
	case 1:
		result.Reason = "insufficient_history"
		return result, true
	case 2:
		result.Reason = "stale_baseline"
		return result, true
	}
	result.Ready = true
	result.Confidence = "moderate"
	if raw.confidence == 2 {
		result.Confidence = "high"
	} else if raw.confidence == 3 {
		result.Confidence = "guarded"
	}
	result.Baseline, result.Scale, result.Score = float64(raw.baseline), float64(raw.scale), float64(raw.score)
	result.Anomalous = math.Abs(result.Score) >= healthbaseline.AlertScore
	if result.Score > 0 {
		result.Direction = "above"
	} else if result.Score < 0 {
		result.Direction = "below"
	}
	return result, true
}

func coreQuery(s *coreSeries, from, to int64) (health.Statistics, bool) {
	var result C.yihe_result
	var exact C.uint8_t
	if s.exactInteger {
		exact = 1
	}
	status := C.yihe_core_query_v1(
		(*C.int64_t)(unsafe.Pointer(unsafe.SliceData(s.timestamps))),
		(*C.double)(unsafe.Pointer(unsafe.SliceData(s.values))),
		C.size_t(len(s.values)), C.int64_t(from), C.int64_t(to), exact, &result)
	switch status {
	case 0:
		return health.Statistics{}, true
	case 1:
		return health.Statistics{Count: int(result.count), Unit: s.unit,
			Min: health.Number(float64(result.min)), Max: health.Number(float64(result.max)),
			Mean: health.Number(float64(result.mean))}, true
	default:
		return health.Statistics{}, false
	}
}
