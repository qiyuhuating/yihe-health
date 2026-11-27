package healthbaseline

import (
	"errors"
	"math"
	"math/rand"
	"slices"
	"testing"
	"time"
)

func TestContiguousBucketsDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(20261126))
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 1, DeviceID: 2, BindingVersion: 3, Source: "watch", SourceDevice: "A", Metric: "heart_rate", At: now, Value: 91, Valid: true}
	for _, n := range []int{0, 28, 224, 896, 897, 40320, 60000} {
		history := make([]Observation, n)
		for i := range history {
			s := current
			age := 1 + rng.Intn(28)
			if i%11 == 0 {
				age = 1
			} // intentionally skew one bucket past its reserve
			s.At = now.Truncate(24*time.Hour).AddDate(0, 0, -age).Add(time.Duration(rng.Intn(86400)) * time.Second)
			s.Value = float64(40 + rng.Intn(140))
			if i%13 == 0 {
				s.OwnerID = 7
			}
			if i%19 == 0 {
				s.BindingVersion = 4
			}
			if i%23 == 0 {
				s.Valid = false
			}
			history[i] = s
		}
		before := slices.Clone(history)
		want, e1 := evaluateLegacy(current, history)
		got, e2 := Evaluate(current, history)
		if e1 != e2 || got != want || !slices.Equal(history, before) {
			t.Fatalf("n=%d got=%+v %v want=%+v %v", n, got, e2, want, e1)
		}
	}
}
func evaluateLegacy(current Observation, history []Observation) (Result, error) {
	if current.OwnerID <= 0 || current.BindingVersion <= 0 || current.Metric == "" ||
		current.At.IsZero() || !current.Valid || math.IsNaN(current.Value) || math.IsInf(current.Value, 0) {
		return Result{}, errors.New("invalid current health observation")
	}
	const dayDuration = 24 * time.Hour
	day := current.At.UTC().Truncate(dayDuration)
	firstDay := day.AddDate(0, 0, -HistoryDays)

	// The window has a fixed number of UTC calendar days, already in time order.
	var days [HistoryDays][]float64
	var firstValues [HistoryDays]float64
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
			firstValues[i] = sample.Value
			days[i] = firstValues[i : i+1 : i+1]
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
