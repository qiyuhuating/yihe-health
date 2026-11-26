package health

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestSleepDailyTotalOverflowIsExplicit(t *testing.T) {
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	a := Sample{RecordID: "sleep-a", Source: "watch", SourceDevice: "A", Metric: "sleep_duration", Unit: "min", Semantics: "daily_total", Start: from, End: from, Version: 1, Value: Number(1e308)}
	b := a
	b.RecordID, b.Start, b.End = "sleep-b", from.Add(24*time.Hour), from.Add(24*time.Hour)
	for _, sample := range []Sample{a, b} {
		if _, err := sample.Normalize(); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := Aggregate([]Sample{a, b}, from, from.Add(48*time.Hour)); err == nil {
		t.Errorf("unrepresentable total accepted: %+v", out)
	}
	b.Value = Number(1)
	out, err := Aggregate([]Sample{a, b}, from, from.Add(48*time.Hour))
	if err != nil || out.Count != 2 || out.Total == nil || *out.Total != 1e308 {
		t.Fatalf("finite total rejected: %+v err=%v", out, err)
	}
	if _, err := json.Marshal(out); err != nil {
		t.Fatal(err)
	}
}

func TestContinuousDailyConflictsIndependentOfInputOrder(t *testing.T) {
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	a := Sample{RecordID: "a", Source: "watch", SourceDevice: "A", Metric: "steps", Unit: "count", Semantics: "daily_total", Start: from.Add(8 * time.Hour), Value: Number(100)}
	b, latest := a, a
	b.RecordID, b.Value = "b", Number(200)
	latest.RecordID, latest.Start, latest.Value = "latest", from.Add(9*time.Hour), Number(300)
	samples := []Sample{a, b, latest}
	for _, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		input := []Sample{samples[order[0]], samples[order[1]], samples[order[2]]}
		if out, err := Aggregate(input, from, from.Add(24*time.Hour)); err == nil {
			t.Errorf("order %v silently accepted conflicting observations: %+v", order, out)
		}
	}
	b.Start = b.Start.In(time.FixedZone("same instant", 2*3600))
	if _, err := Aggregate([]Sample{latest, a, b}, from, from.Add(24*time.Hour)); err == nil {
		t.Error("different RFC3339 offsets hid a same-instant conflict")
	}
	b.Value = Number(100)
	if out, err := Aggregate([]Sample{latest, a, b}, from, from.Add(24*time.Hour)); err != nil || out.Count != 1 || out.Total == nil || *out.Total != 300 {
		t.Fatalf("equal duplicate displaced the latest daily snapshot: %+v err=%v", out, err)
	}
	b.Value, b.Deleted = Number(200), true
	if out, err := Aggregate([]Sample{latest, a, b}, from, from.Add(24*time.Hour)); err != nil || out.Total == nil || *out.Total != 300 {
		t.Fatalf("deleted conflicting observation affected aggregation: %+v err=%v", out, err)
	}
}

func TestContinuousFiniteScalarMeanSurvivesSumOverflow(t *testing.T) {
	for _, values := range [][]float64{{1e308, 1e308}, {math.MaxFloat64, math.MaxFloat64, math.MaxFloat64}, {math.MaxFloat64, math.MaxFloat64 / 2}} {
		samples := make([]Sample, len(values))
		for i, value := range values {
			samples[i] = Sample{Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Value: Number(value)}
		}
		out, err := Aggregate(samples, time.Time{}, time.Time{})
		want := values[0]
		if len(values) == 2 && values[0] != values[1] {
			want = math.MaxFloat64 * 0.75
		}
		if err != nil || out.Mean == nil || math.IsInf(*out.Mean, 0) || math.IsNaN(*out.Mean) || math.Abs(*out.Mean/want-1) > 1e-15 {
			t.Errorf("finite mean overflowed: values=%v result=%+v err=%v", values, out, err)
		}
	}
}
