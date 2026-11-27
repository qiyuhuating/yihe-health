package handlers

import (
	"fmt"
	"testing"
	"time"
	"yihe-care/internal/health"
)

func BenchmarkApplyCoreChanges(b *testing.B) {
	const n = 4096
	from := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	s := &coreSeries{
		key: coreKey{1, "watch", "WATCH001"}, revision: n, from: from.UnixNano(),
		to: from.Add(n * time.Second).UnixNano(), unit: "count/min", exactInteger: true,
		timestamps: make([]int64, n), values: make([]float64, n), ids: make([]string, n),
	}
	for i := range n {
		s.timestamps[i] = from.Add(time.Duration(i) * time.Second).UnixNano()
		s.values[i] = 72
		s.ids[i] = fmt.Sprintf("%064x", i)
	}
	for _, scenario := range []string{"correction", "outside"} {
		changes := make([]health.Record, 128)
		for i := range changes {
			id := s.ids[i*17]
			at := from.Add(time.Duration(i*17) * time.Second)
			if scenario == "outside" {
				id = fmt.Sprintf("%064x", n+i)
				at = from.Add(2*n*time.Second + time.Duration(i)*time.Second)
			}
			changes[i] = health.Record{SampleID: id, OldID: 1, Revision: int64(n + i + 1), Sample: health.Sample{Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Semantics: "instant", Unit: "count/min", Start: at, Value: health.Number(78)}}
		}
		b.Run(scenario, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				next := applyCoreChanges(s, n+128, changes, from)
				if next == nil || len(next.values) != n || next.revision != n+128 {
					b.Fatal("invalid projection")
				}
			}
		})
	}
}
