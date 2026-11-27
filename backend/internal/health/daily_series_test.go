package health

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestDailySeriesDifferential(t *testing.T) {
	r := rand.New(rand.NewSource(773))
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	for _, metric := range []string{"heart_rate", "steps", "sleep_stage"} {
		for _, semantics := range []string{"instant", "interval", "daily_total"} {
			for trial := 0; trial < 100; trial++ {
				var stream dailySeries
				var samples []Sample
				for i := 0; i < r.Intn(64); i++ {
					s := Sample{Metric: metric, Semantics: semantics, Source: "watch", SourceDevice: "A", Unit: "count", Stage: "core", Start: from.Add(time.Duration(i) * time.Minute), End: from.Add(time.Duration(i+1) * time.Minute), Value: Number([]float64{0, math.Copysign(0, -1), 1, 100, 1e308, -1e308, math.NaN()}[r.Intn(7)]), Deleted: r.Intn(8) == 0}
					if r.Intn(15) == 0 {
						s.Value = nil
					}
					if r.Intn(15) == 0 {
						s.Source = "other"
					}
					if r.Intn(20) == 0 {
						s.TimeBasis = "legacy-unzoned"
					}
					stream.add(s)
					samples = append(samples, s)
				}
				want, werr := Aggregate(samples, from, from.Add(24*time.Hour))
				got, gerr := stream.result(from, from.Add(24*time.Hour))
				if fmt.Sprint(werr) != fmt.Sprint(gerr) || !equalStatistics(want, got) {
					t.Fatalf("metric=%s semantics=%s trial=%d want=%+v %v got=%+v %v", metric, semantics, trial, want, werr, got, gerr)
				}
			}
		}
	}
}

func BenchmarkDailyScalarMaterialization(b *testing.B) {
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	value := 72.0
	sample := Sample{Metric: "heart_rate", Semantics: "instant", Source: "watch", SourceDevice: "A", Unit: "count/min", Value: &value, Start: from, End: from}
	for _, scenario := range []string{"materialize", "stream"} {
		b.Run(scenario, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if scenario == "materialize" {
					var samples []Sample
					for range 10000 {
						samples = append(samples, sample)
					}
					benchmarkAggregate, _ = Aggregate(samples, from, from.Add(24*time.Hour))
				} else {
					var stream dailySeries
					for range 10000 {
						stream.add(sample)
					}
					benchmarkAggregate, _ = stream.result(from, from.Add(24*time.Hour))
				}
			}
		})
	}
}
