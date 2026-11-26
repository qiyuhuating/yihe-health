//go:build linux && amd64 && cgo && !purego

package health

import (
	"slices"
	"testing"
	"time"
)

func TestNativeSleepPermutation(t *testing.T) {
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	samples := make([]*Sample, 4096)
	original := make(map[*Sample]bool, len(samples))
	for i := range samples {
		// Include equal seconds with distinct nanoseconds and repeated starts.
		delta := (i * 7919) % 1536
		samples[i] = &Sample{Start: start.Add(time.Duration(delta/8)*time.Second + time.Duration(delta%8))}
		original[samples[i]] = true
	}
	samples[0], samples[1] = samples[1], samples[0]
	if !sortSleepNative(samples) {
		t.Fatal("eligible input did not use native sort")
	}
	if !slices.IsSortedFunc(samples, func(a, b *Sample) int { return a.Start.Compare(b.Start) }) {
		t.Fatal("incorrect chronological order")
	}
	for _, sample := range samples {
		if !original[sample] {
			t.Fatal("duplicate or unknown sample after permutation")
		}
		delete(original, sample)
	}
	samples[len(samples)-1].Start = time.Now()
	if sortSleepNative(samples) {
		t.Fatal("expected Go fallback for monotonic timestamps")
	}
}

func TestNativeSortWideRangeFallback(t *testing.T) {
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	samples := make([]*Sample, 2048)
	for i := range samples {
		samples[i] = &Sample{Start: start.Add(-time.Duration(i) * time.Hour)}
	}
	original := slices.Clone(samples)
	if sortSleepNative(samples) || !slices.Equal(samples, original) {
		t.Fatal("wide span must fall back before changing sample order")
	}
}
