package health

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func TestSleepSortedUnionDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(102601))
	stages := []string{"asleep", "core", "deep", "rem", "awake", "in_bed", "", "unrecognized"}
	for trial := 0; trial < 1200; trial++ {
		from := time.Date([]int{1, 2026, 9999}[trial%3], 10, 26, 0, 0, 0, 0, time.FixedZone("reporting", (trial%29-14)*3600))
		to := from.Add(24 * time.Hour)
		if trial%17 == 0 {
			to = from
		}
		if trial%19 == 0 {
			to = from.Add(-time.Hour)
		}
		samples := make([]Sample, rng.Intn(200))
		for i := range samples {
			start := from.Add(time.Duration(rng.Intn(1800)-300)*time.Minute + time.Duration(rng.Intn(1000)))
			// Duplicate and touching endpoints, nested intervals and backwards
			// raw inputs exercise the same clipping decisions as the old API.
			if i%5 == 0 {
				start = from.Add(-time.Minute)
			}
			s := Sample{Metric: "sleep_stage", Source: "watch", SourceDevice: "A", Semantics: "interval", Unit: "min", Value: Number(0), Start: start, End: start.Add(time.Duration(rng.Intn(400)-10)*time.Minute + time.Duration(rng.Intn(1000))), Stage: stages[rng.Intn(len(stages))]}
			if i%7 == 0 {
				s.End = from.Add(24*time.Hour + time.Minute)
			}
			if rng.Intn(10) == 0 {
				s.Deleted = true
			}
			if rng.Intn(10) == 0 {
				s.Value = nil
			}
			// Aggregate historically accepts this unusual raw combination;
			// daily selection must still happen before the sleep union.
			if trial%11 == 0 {
				s.Semantics = "daily_total"
			}
			samples[i] = s
		}
		before := append([]Sample{}, samples...)
		got, err := Aggregate(samples, from, to)
		want, refErr := referenceAggregate(samples, from, to)
		if fmt.Sprint(err) != fmt.Sprint(refErr) || !equalStatistics(got, want) {
			t.Fatalf("trial=%d got=%+v err=%v want=%+v err=%v", trial, got, err, want, refErr)
		}
		if !reflect.DeepEqual(samples, before) {
			t.Fatalf("trial=%d modified caller input", trial)
		}
	}
}

// All allocations for the decoded source array are outside the measurement.
// Only valid clipped intervals should enter the sort; sparse/outside controls
// protect against trading large transient arrays for unnecessary sorting.
func BenchmarkSleepWindowLayout(b *testing.B) {
	from := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for _, n := range []int{10, 1000, 100000} {
		for _, scenario := range []string{"disjoint", "outside", "sparse"} {
			samples := make([]Sample, n)
			for i := range samples {
				start := from.Add(time.Duration((i*7919)%n) * time.Second)
				s := Sample{Metric: "sleep_stage", Source: "watch", SourceDevice: "A", Semantics: "interval", Unit: "min", Value: Number(0), Start: start, End: start.Add(500 * time.Millisecond), Stage: []string{"deep", "awake", "core"}[i%3]}
				if scenario == "outside" {
					s.Start = s.Start.Add(-72 * time.Hour)
					s.End = s.End.Add(-72 * time.Hour)
				}
				if scenario == "sparse" {
					s.Deleted = i != n/2
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
