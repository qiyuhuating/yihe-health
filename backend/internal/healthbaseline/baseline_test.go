package healthbaseline

import (
	"math"
	"testing"
	"time"
)

func TestPersonalBaseline(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 7, DeviceID: 19, BindingVersion: 2, Source: "watch", SourceDevice: "watch19", Metric: "resting_heart_rate", At: now, Value: 80, Valid: true}
	var history []Observation
	for i := 1; i <= 8; i++ {
		s := current
		s.At = now.AddDate(0, 0, -i)
		s.Value = 60
		history = append(history, s)
	}
	// Neither a second owner nor a previous binding epoch may pollute a baseline.
	wrong := current
	wrong.OwnerID = 8
	wrong.At = now.AddDate(0, 0, -1)
	wrong.Value = 120
	history = append(history, wrong)
	wrong.OwnerID = 7
	wrong.BindingVersion = 1
	history = append(history, wrong)
	wrong = current
	wrong.DeviceID = 20
	wrong.At = now.AddDate(0, 0, -2)
	wrong.Value = 120
	history = append(history, wrong)
	wrong = current
	wrong.SourceDevice = "different-sensor"
	wrong.At = now.AddDate(0, 0, -3)
	wrong.Value = 120
	history = append(history, wrong)
	result, err := Evaluate(current, history)
	if err != nil || !result.Ready || !result.Anomalous || result.Baseline != 60 || result.Direction != "above" || result.SampleDays != 8 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestInsufficientHistoryAndInvalidData(t *testing.T) {
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 7, BindingVersion: 2, Metric: "steps", At: now, Value: 500, Valid: true}
	history := make([]Observation, 6)
	for i := range history {
		history[i] = current
		history[i].At = now.AddDate(0, 0, -(i + 1))
		history[i].Value = 1000
	}
	result, err := Evaluate(current, history)
	if err != nil || result.Ready || result.SampleDays != 6 {
		t.Fatalf("cold start: %+v %v", result, err)
	}
	current.Value = math.NaN()
	if _, err := Evaluate(current, history); err == nil {
		t.Fatal("accepted NaN")
	}
}

func TestDailyMedianAndHistoryWindow(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 1, BindingVersion: 1, Metric: "resting_heart_rate", At: now, Value: 61, Valid: true}
	var history []Observation
	for i := 1; i <= MinimumDays; i++ {
		s := current
		s.At = now.AddDate(0, 0, -i)
		s.Value = 60
		history = append(history, s)
	}
	outlier := history[0]
	outlier.Value = 200
	history = append(history, outlier)
	stale := history[0]
	stale.At = now.AddDate(0, 0, -29)
	stale.Value = 500
	history = append(history, stale)
	result, err := Evaluate(current, history)
	if err != nil || !result.Ready || result.Baseline != 60 || result.Anomalous {
		t.Fatalf("daily aggregate/window: %+v %v", result, err)
	}
}

func TestSustainedAnomalyCannotPoisonBaseline(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 1, DeviceID: 2, BindingVersion: 1, Source: "watch", SourceDevice: "w2", Metric: "heart_rate", At: now, Value: 112, Valid: true}
	var history []Observation
	for age := 1; age <= 21; age++ {
		s := current
		s.At = now.AddDate(0, 0, -age)
		if age > 14 {
			s.Value = 70
		} else {
			s.Value = 112
		}
		history = append(history, s)
	}
	got, err := Evaluate(current, history)
	if err != nil || got.Ready || got.Reason != "stale_baseline" || got.Baseline != 0 || got.RejectedDays != 14 {
		t.Fatalf("untrusted recent history must not normalize prolonged anomaly: %+v err=%v", got, err)
	}
	// The reference must remain correct even if the last excursion is recent.
	current.At = now.AddDate(0, 0, -11)
	got, err = Evaluate(current, history)
	if err != nil || !got.Ready || !got.Anomalous || got.Baseline != 70 || got.RejectedDays != 3 || got.Confidence != "guarded" {
		t.Fatalf("anchored baseline: %+v err=%v", got, err)
	}
}

func TestBaselineFreshnessAndConfidence(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 1, BindingVersion: 1, Metric: "heart_rate", At: now, Value: 72, Valid: true}
	var history []Observation
	for i := 10; i <= 16; i++ {
		s := current
		s.At = now.AddDate(0, 0, -i)
		history = append(history, s)
	}
	got, err := Evaluate(current, history)
	if err != nil || got.Ready || got.Reason != "stale_baseline" {
		t.Fatalf("stale confidence: %+v err=%v", got, err)
	}
}
