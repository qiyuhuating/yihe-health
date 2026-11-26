//go:build yihecore && cgo && !purego && ((windows && amd64) || (linux && (amd64 || arm64)))

package handlers

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"yihe-care/internal/healthbaseline"
)

func baselineFixture(n int) (healthbaseline.Observation, []healthbaseline.Observation) {
	current := healthbaseline.Observation{OwnerID: 1, DeviceID: 2, BindingVersion: 3,
		Source: "watch", SourceDevice: "synthetic", Metric: "heart_rate",
		At: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC), Value: 92, Valid: true}
	history := make([]healthbaseline.Observation, n)
	for i := range history {
		s := current
		s.At = current.At.AddDate(0, 0, -1-i%28).Add(time.Duration(i%12) * time.Minute)
		s.Value = float64(60 + (i*7919)%40)
		history[i] = s
	}
	return current, history
}

func TestCoreBaselineParity(t *testing.T) {
	rng := rand.New(rand.NewSource(211))
	for trial := range 200 {
		current, history := baselineFixture(672)
		for i := range history {
			history[i].Value += float64(rng.Intn(8)) / 8
			if trial%4 == 0 && i%28 < 14 {
				history[i].Value += 100
			}
			if trial%7 == 0 && i%28 < 10 {
				history[i].BindingVersion--
			}
			if i%17 == 0 {
				history[i].DeviceID++
			}
		}
		rng.Shuffle(len(history), func(i, j int) { history[i], history[j] = history[j], history[i] })
		want, err := healthbaseline.Evaluate(current, history)
		got, ok := coreBaseline(current, history)
		if err != nil || !ok || got != want {
			t.Fatalf("trial %d: native=%+v go=%+v err=%v native=%v", trial, got, want, err, ok)
		}
	}
}

func BenchmarkCoreBaseline(b *testing.B) {
	for _, n := range []int{672, 5000} {
		current, history := baselineFixture(n)
		b.Run(fmt.Sprintf("Go/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_, _ = healthbaseline.Evaluate(current, history)
			}
		})
		b.Run(fmt.Sprintf("Rust/%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_, _ = coreBaseline(current, history)
			}
		})
	}
}
