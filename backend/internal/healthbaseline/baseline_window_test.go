package healthbaseline

import (
	"slices"
	"testing"
	"time"
)

func TestSparseUTCWindowAndInputPreservation(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	current := Observation{OwnerID: 1, BindingVersion: 1, Metric: "heart_rate", At: now, Value: 90, Valid: true}
	var history []Observation
	for _, age := range []int{7, 28, 12, 10, 9, 20, 8} {
		for _, value := range []float64{64, 56} {
			s := current
			s.At = now.Truncate(24*time.Hour).AddDate(0, 0, -age).In(time.FixedZone("east", 8*3600))
			s.Value = value
			history = append(history, s)
		}
	}
	for _, age := range []int{0, 29, -1} {
		s := current
		s.At = now.AddDate(0, 0, -age)
		s.Value = 200
		history = append(history, s)
	}
	before := slices.Clone(history)
	got, err := Evaluate(current, history)
	want := Result{Ready: true, SampleDays: 7, TrustedDays: 7, Confidence: "moderate", Baseline: 60, Scale: 5, Score: 6, Direction: "above", Anomalous: true}
	if err != nil || got != want {
		t.Fatalf("got %+v, err=%v; want %+v", got, err, want)
	}
	if !slices.Equal(history, before) {
		t.Fatal("evaluation mutated input history")
	}
}
