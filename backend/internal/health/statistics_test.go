package health

import (
	"fmt"
	"reflect"
	"testing"
	"time"
	_ "time/tzdata"
)

func TestSleepUnionAndConflictingStages(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	makeStage := func(a, b time.Duration, stage string) Sample {
		return Sample{Metric: "sleep_stage", Source: "watch", SourceDevice: "A", Semantics: "interval", Unit: "min", Value: Number(0), Start: from.Add(a), End: from.Add(b), Stage: stage}
	}
	samples := []Sample{makeStage(time.Hour, 3*time.Hour, "asleep"), makeStage(time.Hour, 2*time.Hour, "deep"), makeStage(2*time.Hour, 3*time.Hour, "core"), makeStage(0, 4*time.Hour, "in_bed"), makeStage(90*time.Minute, 2*time.Hour, "awake")}
	out, err := Aggregate(samples, from, to)
	if err != nil || out.AsleepMinutes != 120 || out.StageMinutes["deep"] != 60 || out.ConflictingSleepMinutes != 30 {
		t.Fatalf("%+v %v", out, err)
	}
}

// The formatted-date oracle preserves the original calendar grouping without
// depending on the tuple used by Aggregate.
func TestAggregateDailyCalendarEquivalent(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Australia/Lord_Howe"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		for _, year := range []int{-10000, -1, 0, 1, 2024, 2026, 9999, 10000} {
			for _, month := range []time.Month{time.February, time.March, time.November, time.December} {
				from := time.Date(year, month, 27, 0, 0, 0, 0, loc)
				samples := make([]Sample, 0, 80)
				daily := map[string]Sample{}
				for i := 79; i >= 0; i-- {
					s := reviewStep(from.Add(time.Duration(i)*3*time.Hour).In(time.UTC), float64(i))
					samples = append(samples, s)
					day := s.Start.In(loc).Format("2006-01-02")
					if old, ok := daily[day]; !ok || s.Start.After(old.Start) {
						daily[day] = s
					}
				}
				total, low, high := 0.0, 100.0, -1.0
				for _, s := range daily {
					v := *s.Value
					total += v
					if v < low {
						low = v
					}
					if v > high {
						high = v
					}
				}
				want := Statistics{Count: len(daily), Unit: "count", Total: Number(total), Min: Number(low), Max: Number(high)}
				got, err := Aggregate(samples, from, from.AddDate(0, 0, 11))
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("zone=%s year=%d month=%d got=%+v err=%v want=%+v", zone, year, month, got, err, want)
				}
			}
		}
	}
}

func TestAggregatePreservesCallerSamples(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, semantics := range []string{"instant", "interval", "daily_total"} {
		samples := []Sample{reviewStep(from.Add(2*time.Hour), 20), reviewStep(from.Add(time.Hour), 10)}
		for i := range samples {
			samples[i].Semantics = semantics
			samples[i].End = samples[i].Start.Add(time.Minute)
		}
		before := append([]Sample(nil), samples...)
		if _, err := Aggregate(samples, from, from.Add(24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(samples, before) || *samples[0].Value != 20 || *samples[1].Value != 10 {
			t.Fatalf("%s aggregation mutated caller samples", semantics)
		}
	}
}

var benchmarkAggregate Statistics

// Decoded samples only; this excludes database reads and JSON decoding.
func BenchmarkAggregate(b *testing.B) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, n := range []int{10, 1000} {
		for _, scenario := range []string{"instant", "interval", "daily_total", "sparse", "sleep"} {
			samples := make([]Sample, n)
			for i := range samples {
				s := reviewStep(from.Add(time.Duration(i)*time.Minute), float64(i+1))
				s.Semantics = scenario
				s.End = s.Start.Add(time.Minute)
				switch scenario {
				case "instant":
					s.Metric = "heart_rate"
				case "sparse":
					s.Semantics = "instant"
					s.Deleted = i%100 != 0
				case "sleep":
					s.Semantics, s.Metric, s.Stage = "interval", "sleep_stage", "core"
				}
				samples[i] = s
			}
			b.Run(fmt.Sprintf("%s/%d", scenario, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var err error
					benchmarkAggregate, err = Aggregate(samples, from, from.Add(24*time.Hour))
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func TestStepsDoNotMixCumulativeOrOverlappingIntervals(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	s := Sample{Metric: "steps", Source: "watch", SourceDevice: "A", Semantics: "daily_total", Unit: "count", Value: Number(100), Start: from.Add(time.Hour)}
	next := s
	next.Start = from.Add(2 * time.Hour)
	next.Value = Number(120)
	out, err := Aggregate([]Sample{s, next}, from, to)
	if err != nil || out.Total == nil || *out.Total != 120 || out.Count != 1 {
		t.Fatalf("snapshots were summed: %+v %v", out, err)
	}
	s.Semantics = "interval"
	s.End = from.Add(3 * time.Hour)
	next.Semantics = "interval"
	next.End = from.Add(4 * time.Hour)
	if _, err = Aggregate([]Sample{s, next}, from, to); err == nil {
		t.Fatal("overlapping steps silently summed")
	}
	s.Semantics = "daily_total"
	if _, err = Aggregate([]Sample{s, next}, from, to); err == nil {
		t.Fatal("mixed sampling semantics accepted")
	}
}
