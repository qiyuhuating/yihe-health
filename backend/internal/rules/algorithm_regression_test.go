package rules

import (
	"yihe-care/internal/health"
	"testing"
	"time"
)

func TestAlgorithmSameDayCounterRollback(t *testing.T) {
	p := Default()
	loc, _ := time.LoadLocation(p.Timezone)
	from := time.Date(2026, 10, 18, 22, 0, 0, 0, loc)
	var w activityWindow
	for i, v := range []float64{1000, 900, 1000} {
		at := from.Add(time.Duration(i+1) * time.Minute)
		w.add(p, health.Sample{Source: "watch", SourceDevice: "A", Metric: "steps", Semantics: "daily_total", Value: health.Number(v), Start: at, End: at}, from, from.Add(15*time.Minute))
	}
	if v, ok := w.value(); ok {
		t.Fatalf("unconfirmed same-day rollback accepted as %v steps", v)
	}
}
func TestAlgorithmMidnightResetWithoutValueDrop(t *testing.T) {
	p := Default()
	loc, _ := time.LoadLocation(p.Timezone)
	from := time.Date(2026, 10, 18, 23, 55, 0, 0, loc)
	var w activityWindow
	for i, v := range []float64{100, 250} {
		at := from.Add(time.Duration(4+i*6) * time.Minute)
		w.add(p, health.Sample{Source: "watch", SourceDevice: "A", Metric: "steps", Semantics: "daily_total", Value: health.Number(v), Start: at, End: at}, from, from.Add(15*time.Minute))
	}
	if v, ok := w.value(); !ok || v != 250 {
		t.Fatalf("got (%v,%v); device resets daily: observed new-day increment at least250, default alert threshold200", v, ok)
	}
}
func TestAlgorithmBaselineFloors(t *testing.T) {
	p := Default()
	p.BaselinePercent = 1
	p.BaselineSigma = 1
	now := time.Date(2026, 10, 18, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		delta float64
		want  bool
	}{{8, false}, {8.1, true}} {
		in := Input{Now: now, Heart: &Value{Value: 60 + tc.delta, At: now}, Baselines: map[string]Baseline{"heart_rate": {Count: 20, Mean: 60, Deviation: 0}}}
		for _, c := range EvaluateCompact(p, in) {
			if c.Kind == "baseline_heart_rate" && (c.Breach != tc.want || c.evidence.threshold != 8) {
				t.Fatalf("heart floor failed: %+v", c)
			}
		}
	}
	for _, tc := range []struct {
		delta float64
		want  bool
	}{{12, false}, {12.1, true}} {
		in := Input{Now: now, Blood: &Value{Value: 110 + tc.delta, At: now}, Baselines: map[string]Baseline{"blood_pressure": {Count: 20, Mean: 110, Deviation: 0}}}
		for _, c := range EvaluateCompact(p, in) {
			if c.Kind == "baseline_blood_pressure" && (c.Breach != tc.want || c.evidence.threshold != 12) {
				t.Fatalf("blood floor failed: %+v", c)
			}
		}
	}
}

func TestActivityCounterDayTimezone(t *testing.T) {
	for _, tc := range []struct {
		name, zone   string
		offsetChange bool
		want         float64
		ok           bool
	}{
		{"explicit_counter_day", "Asia/Shanghai", false, 250, true},
		{"explicit_zone_mixed_encoding", "Asia/Shanghai", true, 250, true},
		{"invalid_zone", "Invalid/Timezone", false, 0, false},
		{"local_zone", "Local", false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Default()
			p.Timezone = "Asia/Tokyo" // Both observations share the rule date.
			from := time.Date(2026, 10, 18, 15, 55, 0, 0, time.UTC)
			var w activityWindow
			for i, v := range []float64{100, 250} {
				at := from.Add(time.Duration(4+i*6) * time.Minute)
				if tc.offsetChange && i == 1 {
					at = at.In(time.FixedZone("", 8*3600))
				}
				w.add(p, health.Sample{Source: "watch", SourceDevice: "A", Metric: "steps", Semantics: "daily_total", Value: health.Number(v), Start: at, End: at, Metadata: map[string]string{"counterTimezone": tc.zone}}, from, from.Add(15*time.Minute))
			}
			got, ok := w.value()
			if ok != tc.ok || ok && got != tc.want {
				t.Fatalf("got(%v,%v) want(%v,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
func TestActivityCounterResetEdges(t *testing.T) {
	for _, tc := range []struct {
		name    string
		values  []float64
		minutes []int
		want    float64
		ok      bool
	}{
		{"midnight_drop", []float64{5000, 20, 45}, []int{4, 5, 10}, 45, true},
		{"midnight_no_drop", []float64{100, 250}, []int{4, 10}, 250, true},
		{"same_day_rollback", []float64{1000, 900, 1000}, []int{0, 1, 2}, 0, false},
		{"same_time_conflict", []float64{100, 150}, []int{0, 0}, 0, false},
		{"same_time_duplicate", []float64{100, 100, 110}, []int{0, 0, 1}, 10, true},
		{"out_of_order", []float64{100, 110}, []int{1, 0}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Default()
			loc, _ := time.LoadLocation(p.Timezone)
			from := time.Date(2026, 10, 18, 23, 55, 0, 0, loc)
			var w activityWindow
			for i, v := range tc.values {
				at := from.Add(time.Duration(tc.minutes[i]) * time.Minute)
				w.add(p, health.Sample{Source: "watch", SourceDevice: "A", Metric: "steps", Semantics: "daily_total", Value: health.Number(v), Start: at, End: at}, from, from.Add(15*time.Minute))
			}
			got, ok := w.value()
			if ok != tc.ok || ok && got != tc.want {
				t.Fatalf("got(%v,%v) want(%v,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
func TestActivityIntervalUsesConservedAllocation(t *testing.T) {
	p := Default()
	loc, _ := time.LoadLocation(p.Timezone)
	start := time.Date(2026, 10, 18, 23, 59, 0, 0, loc)
	s := health.Sample{Source: "watch", SourceDevice: "A", Metric: "steps", Semantics: "interval", Value: health.Number(1), Start: start, End: start.Add(2 * time.Minute)}
	sum := 0.0
	for i, want := range []float64{1, 0} {
		var w activityWindow
		from := start.Add(time.Duration(i) * time.Minute)
		w.add(p, s, from, from.Add(time.Minute))
		got, ok := w.value()
		if !ok || got != want {
			t.Fatalf("part%d got(%v,%v) want%v", i, got, ok, want)
		}
		sum += got
	}
	if sum != 1 {
		t.Fatalf("sum%v", sum)
	}
}
