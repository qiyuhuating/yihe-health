// Package healthbaseline evaluates deviations from a person's own recent history.
// It provides decision support, not a medical diagnosis or an automatic SOS trigger.
package healthbaseline

import (
	"errors"
	"math"
	"slices"
	"time"
)

const (
	HistoryDays = 28
	MinimumDays = 7
	AlertScore  = 3.5
)

type Observation struct {
	OwnerID        int64
	DeviceID       int64
	BindingVersion int64
	Source         string
	SourceDevice   string
	Metric         string
	At             time.Time
	Value          float64
	Valid          bool
}

type Result struct {
	Ready        bool
	SampleDays   int
	TrustedDays  int
	RejectedDays int
	Confidence   string
	Reason       string
	Baseline     float64
	Scale        float64
	Score        float64
	Direction    string
	Anomalous    bool
}

// Evaluate compares a valid observation against at most 28 preceding calendar
// days, taking a median for each day so high-frequency sampling cannot dominate.
// Historical samples from a different owner or binding epoch are excluded.
func Evaluate(current Observation, history []Observation) (Result, error) {
	if len(history) <= HistoryDays {
		var sparse [HistoryDays]float64
		return evaluateBuckets(current, history, sparse[:], 1)
	}
	return evaluateDense(current, history)
}

func evaluateDense(current Observation, history []Observation) (Result, error) {
	perDay := min(len(history)/HistoryDays, 2048)
	if perDay <= 32 {
		var small [HistoryDays * 32]float64
		return evaluateBuckets(current, history, small[:], 32)
	}
	return evaluateBuckets(current, history, nil, perDay)
}

func evaluateBuckets(current Observation, history []Observation, scratch []float64, perDay int) (Result, error) {
	if current.OwnerID <= 0 || current.BindingVersion <= 0 || current.Metric == "" ||
		current.At.IsZero() || !current.Valid || math.IsNaN(current.Value) || math.IsInf(current.Value, 0) {
		return Result{}, errors.New("invalid current health observation")
	}
	const dayDuration = 24 * time.Hour
	day := current.At.UTC().Truncate(dayDuration)
	firstDay := day.AddDate(0, 0, -HistoryDays)

	// The window has a fixed number of UTC calendar days, already in time order.
	var days [HistoryDays][]float64

	sampleDays := 0
	for _, sample := range history {
		if sample.OwnerID != current.OwnerID || sample.DeviceID != current.DeviceID || sample.BindingVersion != current.BindingVersion ||
			sample.Source != current.Source || sample.SourceDevice != current.SourceDevice || sample.Metric != current.Metric || !sample.Valid || sample.At.IsZero() ||
			math.IsNaN(sample.Value) || math.IsInf(sample.Value, 0) {
			continue
		}
		if sample.At.Before(firstDay) || !sample.At.Before(day) {
			continue
		}
		i := int(sample.At.Sub(firstDay) / dayDuration)
		if len(days[i]) == 0 {
			sampleDays++
			if scratch == nil {
				scratch = make([]float64, HistoryDays*perDay)
			}
			begin := i * perDay
			scratch[begin] = sample.Value
			days[i] = scratch[begin : begin+1 : begin+perDay]
		} else {
			days[i] = append(days[i], sample.Value)
		}
	}

	result := Result{SampleDays: sampleDays, Confidence: "insufficient", Reason: "insufficient_history"}
	if sampleDays < MinimumDays {
		return result, nil
	}

	// Anchor on the first seven eligible days. Keep the center and scale fixed
	// so a sustained excursion cannot gradually redefine normal.
	var anchor [MinimumDays]float64
	var center, scale float64
	lastTrusted := 0
	for i, values := range days {
		if len(values) == 0 {
			continue
		}
		// Buckets own their samples; sorting cannot mutate the caller's history.
		value := medianInPlace(values)
		if result.TrustedDays < MinimumDays {
			anchor[result.TrustedDays] = value
			result.TrustedDays++
			if result.TrustedDays == MinimumDays {
				center = medianInPlace(anchor[:])
				scale = robustScale(current.Metric, center, anchor[:])
			}
		} else {
			if math.Abs(value-center) >= AlertScore*scale {
				result.RejectedDays++
				continue
			}
			result.TrustedDays++
		}
		lastTrusted = i
	}
	if HistoryDays-lastTrusted > 7 {
		result.Reason = "stale_baseline"
		return result, nil
	}

	score := (current.Value - center) / scale
	result.Ready = true
	result.Reason = ""
	result.Confidence = "moderate"
	if result.TrustedDays >= 14 && result.RejectedDays == 0 {
		result.Confidence = "high"
	}
	if result.RejectedDays > 0 {
		result.Confidence = "guarded"
	}
	result.Baseline = center
	result.Scale = scale
	result.Score = score
	result.Anomalous = math.Abs(score) >= AlertScore
	if score > 0 {
		result.Direction = "above"
	} else if score < 0 {
		result.Direction = "below"
	}
	return result, nil
}

// The anchor is no longer needed after calculating its center.
func robustScale(metric string, center float64, values []float64) float64 {
	for i, value := range values {
		values[i] = math.Abs(value - center)
	}
	floor := math.Max(1, math.Abs(center)*0.05)
	if metric == "heart_rate" || metric == "resting_heart_rate" {
		floor = math.Max(floor, 5)
	}
	return math.Max(floor, 1.4826*medianInPlace(values))
}

func medianInPlace(values []float64) float64 {
	slices.Sort(values)
	mid := len(values) / 2
	if len(values)%2 == 1 {
		return values[mid]
	}
	return values[mid-1]/2 + values[mid]/2
}
