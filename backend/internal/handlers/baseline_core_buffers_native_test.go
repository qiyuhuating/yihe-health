//go:build yihecore && cgo && !purego && ((windows && amd64) || (linux && (amd64 || arm64)))

package handlers

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/healthbaseline"
)

func TestCoreBaselineScratchConcurrency(t *testing.T) {
	current, history := baselineFixture(672)
	day := current.At.UTC().Truncate(24 * time.Hour)
	first := day.AddDate(0, 0, -healthbaseline.HistoryDays)
	history[0].At = first.Add(-time.Nanosecond)
	history[1].At = first.In(time.FixedZone("west", -7*3600))
	history[2].At = day.Add(-time.Nanosecond).In(time.FixedZone("east", 8*3600))
	history[3].At = day
	want, err := healthbaseline.Evaluate(current, history)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 32 {
				got, ok := coreBaseline(current, history)
				if !ok || got != want {
					t.Errorf("native=%+v want=%+v ok=%v", got, want, ok)
					return
				}
			}
		})
	}
	wg.Wait()
}

func BenchmarkCoreBaselineParallel(b *testing.B) {
	for _, n := range []int{672, 5000} {
		current, history := baselineFixture(n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, ok := coreBaseline(current, history); !ok {
						b.Error("native failed")
						return
					}
				}
			})
		})
	}
}
