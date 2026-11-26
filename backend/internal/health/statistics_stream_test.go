package health

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"
)

func equalStatistics(a, b Statistics) bool {
	if a.Count != b.Count || a.Unit != b.Unit || len(a.StageMinutes) != len(b.StageMinutes) || (a.StageMinutes == nil) != (b.StageMinutes == nil) || math.Float64bits(a.AsleepMinutes) != math.Float64bits(b.AsleepMinutes) || math.Float64bits(a.ConflictingSleepMinutes) != math.Float64bits(b.ConflictingSleepMinutes) {
		return false
	}
	for _, pair := range [][2]*float64{{a.Min, b.Min}, {a.Max, b.Max}, {a.Mean, b.Mean}, {a.Total, b.Total}} {
		if (pair[0] == nil) != (pair[1] == nil) || pair[0] != nil && math.Float64bits(*pair[0]) != math.Float64bits(*pair[1]) {
			return false
		}
	}
	for key, value := range a.StageMinutes {
		other, ok := b.StageMinutes[key]
		if !ok || math.Float64bits(value) != math.Float64bits(other) {
			return false
		}
	}
	return true
}

func TestAggregateStreamingDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(1026))
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, scenario := range []string{"instant", "duration", "daily_total", "interval", "sleep"} {
		for trial := 0; trial < 600; trial++ {
			samples := make([]Sample, rng.Intn(150))
			for i := range samples {
				s := reviewStep(from.Add(time.Duration(rng.Intn(3000))*time.Minute), float64(rng.Intn(10000)))
				s.Semantics = scenario
				s.End = s.Start.Add(time.Duration(1+rng.Intn(6000))*time.Second + time.Duration(rng.Intn(1000)))
				switch scenario {
				case "instant":
					s.Metric, s.Unit = "heart_rate", "count/min"
					// Include non-associative sums, signed zero and nonfinite
					// values to check the function's existing arithmetic exactly.
					s.Value = Number([]float64{0, math.Copysign(0, -1), 1e16, -1e16, 1e-15, math.Inf(1), math.NaN()}[rng.Intn(7)])
				case "duration":
					s.Metric, s.Semantics, s.Unit = "sleep_duration", "interval", "min"
				case "sleep":
					s.Metric, s.Semantics, s.Unit = "sleep_stage", "interval", "min"
					s.Stage = []string{"asleep", "core", "deep", "rem", "awake", "in_bed"}[rng.Intn(6)]
				}
				if rng.Intn(10) == 0 {
					s.Deleted = true
				}
				if rng.Intn(10) == 0 {
					s.Value = nil
				}
				if trial%3 == 0 && rng.Intn(30) == 0 {
					s.Source = "other"
				}
				if trial%5 == 0 && rng.Intn(30) == 0 {
					s.TimeBasis = "legacy-unzoned"
				}
				samples[i] = s
			}
			got, err := Aggregate(samples, from, from.Add(24*time.Hour))
			want, refErr := referenceAggregate(samples, from, from.Add(24*time.Hour))
			if fmt.Sprint(err) != fmt.Sprint(refErr) || !equalStatistics(got, want) {
				t.Fatalf("scenario=%s trial=%d got=%+v err=%v want=%+v err=%v", scenario, trial, got, err, want, refErr)
			}
		}
	}
}

func BenchmarkAggregateLarge(b *testing.B) {
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, n := range []int{10000, 100000} {
		for _, scenario := range []string{"instant", "daily_total", "sleep", "deleted"} {
			samples := make([]Sample, n)
			for i := range samples {
				s := reviewStep(from.Add(time.Duration((i*7919)%n)*time.Second), 72)
				s.Metric, s.Semantics, s.Unit = "heart_rate", "instant", "count/min"
				if scenario == "daily_total" {
					s.Metric, s.Semantics, s.Unit = "steps", "daily_total", "count"
				}
				if scenario == "deleted" {
					s.Deleted = true
				}
				if scenario == "sleep" {
					s.Metric, s.Semantics, s.Unit = "sleep_stage", "interval", "min"
					s.Stage = []string{"deep", "awake", "core"}[i%3]
					s.End = s.Start.Add(30 * time.Second)
				}
				samples[i] = s
			}
			b.Run(fmt.Sprintf("%s/%d", scenario, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var err error
					benchmarkAggregate, err = Aggregate(samples, from, from.Add(48*time.Hour))
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

// Based on preview@8c22299, with daily timestamp conflicts checked pairwise.
// This oracle remains independent of the production observation map.
func referenceAggregate(samples []Sample, from, to time.Time) (Statistics, error) {
	out := Statistics{}
	type calendarDay struct {
		year  int
		month time.Month
		day   int
	}
	daily := map[calendarDay]*Sample{}
	filtered := []*Sample{}
	for i := range samples {
		s := &samples[i]
		if s.Deleted || s.Value == nil {
			continue
		}
		if s.TimeBasis == "legacy-unzoned" {
			return out, fmt.Errorf("legacy time zone is unknown; use the legacy daily views until reviewed")
		}
		if len(filtered) > 0 {
			a := filtered[0]
			if a.Metric != s.Metric || a.Source != s.Source || a.SourceDevice != s.SourceDevice || a.Semantics != s.Semantics {
				return out, fmt.Errorf("mixed sampling sources or semantics")
			}
		}
		if s.Semantics == "daily_total" {
			// Check all earlier observations independently of which snapshot
			// currently leads its day. Equal instants can use different offsets.
			for j := 0; j < i; j++ {
				previous := &samples[j]
				if !previous.Deleted && previous.Value != nil && s.Start.Equal(previous.Start) && *s.Value != *previous.Value {
					return out, fmt.Errorf("conflicting daily totals at the same observation time")
				}
			}
			year, month, day := s.Start.In(from.Location()).Date()
			key := calendarDay{year, month, day}
			if old, ok := daily[key]; !ok || s.Start.After(old.Start) {
				daily[key] = s
			}
		}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		return out, nil
	}
	first := filtered[0]
	out.Unit = first.Unit
	if first.Semantics == "daily_total" {
		filtered = filtered[:0]
		for _, s := range daily {
			filtered = append(filtered, s)
		}
	}
	out.Count = len(filtered)
	if first.Metric == "sleep_stage" {
		stages := map[string][]referenceInterval{}
		asleep, awake := []referenceInterval{}, []referenceInterval{}
		for _, s := range filtered {
			a, b := s.Start, s.End
			if a.Before(from) {
				a = from
			}
			if b.After(to) {
				b = to
			}
			if !b.After(a) {
				continue
			}
			x := referenceInterval{a, b}
			stages[s.Stage] = append(stages[s.Stage], x)
			switch s.Stage {
			case "asleep", "core", "deep", "rem":
				asleep = append(asleep, x)
			case "awake":
				awake = append(awake, x)
			}
		}
		out.StageMinutes = map[string]float64{}
		for stage, xs := range stages {
			out.StageMinutes[stage] = referenceUnionMinutes(xs)
		}
		out.AsleepMinutes = referenceUnionMinutes(asleep)
		both := append(append([]referenceInterval{}, asleep...), awake...)
		out.ConflictingSleepMinutes = out.AsleepMinutes + referenceUnionMinutes(awake) - referenceUnionMinutes(both)
		return out, nil
	}
	if first.Metric == "steps" && first.Semantics == "interval" {
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Start.Before(filtered[j].Start) })
		total, low, high := 0.0, math.Inf(1), math.Inf(-1)
		out.Count = 0
		for i, s := range filtered {
			if i > 0 && s.Start.Before(filtered[i-1].End) {
				return out, fmt.Errorf("overlapping step intervals require source review")
			}
			v, err := IntervalSteps(*s, from, to)
			if err != nil {
				return out, err
			}
			if !s.End.After(from) || !s.Start.Before(to) {
				continue
			}
			total += v
			if total > 9007199254740991 {
				return out, fmt.Errorf("step total exceeds exact integer range")
			}
			low, high = math.Min(low, v), math.Max(high, v)
			out.Count++
		}
		if out.Count > 0 {
			out.Total, out.Min, out.Max = Number(total), Number(low), Number(high)
		}
		return out, nil
	}
	total, low, high := 0.0, *filtered[0].Value, *filtered[0].Value
	for _, s := range filtered {
		v := *s.Value
		total += v
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
	}
	out.Min = Number(low)
	out.Max = Number(high)
	if first.Semantics == "instant" {
		out.Mean = Number(total / float64(out.Count))
	} else {
		out.Total = Number(total)
	}
	return out, nil
}

// The reference union is kept in test code so production layout changes cannot
// silently change the differential oracle.
type referenceInterval struct{ start, end time.Time }

func referenceUnionMinutes(xs []referenceInterval) float64 {
	if len(xs) == 0 {
		return 0
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].start.Before(xs[j].start) })
	a, b := xs[0].start, xs[0].end
	total := 0.0
	for _, x := range xs[1:] {
		if !x.start.After(b) {
			if x.end.After(b) {
				b = x.end
			}
		} else {
			total += b.Sub(a).Minutes()
			a, b = x.start, x.end
		}
	}
	return total + b.Sub(a).Minutes()
}
