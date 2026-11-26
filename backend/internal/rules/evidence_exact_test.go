package rules

import (
	"bytes"
	"math"
	"math/rand"
	"testing"
	"time"
)

// The optimized serializer must preserve bytes, not just numeric JSON semantics.
func TestFastEvidenceByteExact(t *testing.T) {
	rng := rand.New(rand.NewSource(948223))
	p, in := Default(), benchmarkInput()
	numbers := []float64{math.Copysign(0, -1), 0, 1, -1, 1e-9, 1e-7, 1e-6, 1e20, 1e21, 1e23, 1e26, math.MaxFloat64, math.SmallestNonzeroFloat64, 1.23456, -193.5, float64(math.MaxInt64)}
	sources := []string{"watch", "记录-1", "watch\\escape", "quote\"", "\u2028", "a&b", string([]byte{0xff, 0xfe}), "<tag>"}
	for i := 0; i < 12000; i++ {
		n := numbers[i%len(numbers)]
		in.Heart.Value = n
		in.Blood.Value = n
		in.Heart.Source = sources[i%len(sources)]
		in.Blood.Device = sources[(i+1)%len(sources)]
		in.Battery.Value = n
		in.Activity.Value = n
		in.Baselines["heart_rate"] = Baseline{Count: 20, Mean: n, Deviation: 0}
		at := time.Date(2026, 10, 20, 12, 3, 5, rng.Intn(1000000000), time.UTC)
		if i%7 == 0 {
			at = at.In(time.FixedZone("east", 30))
		}
		if i%41 == 0 {
			at = at.In(time.FixedZone("west", 86400))
		}
		in.Now = at
		in.LastReport = at.Add(-time.Duration(i%300) * time.Second)
		in.Heart.At = at
		in.Blood.At = at
		in.Activity.At = at
		in.Battery.At = at
		for _, c := range EvaluateCompact(p, in) {
			fast, errFast := c.appendEvidenceFast(nil, i%20, int64(i), at)
			canonical, errCanon := c.marshalEvidence(i%20, int64(i), at)
			if (errFast == nil) != (errCanon == nil) || !bytes.Equal(fast, canonical) {
				t.Fatalf("case=%d kind=%s fast=%q err=%v canon=%q err=%v", i, c.Kind, fast, errFast, canonical, errCanon)
			}
			// Verify that the borrowed-buffer path uses identical bytes,
			// including timezone and escaped-string fallback cases.
			var scratch [512]byte
			borrowed, errBorrowed := c.appendEvidenceFast(scratch[:0], i%20, int64(i), at)
			if (errBorrowed == nil) != (errCanon == nil) || !bytes.Equal(borrowed, canonical) {
				t.Fatalf("case=%d kind=%s borrowed=%q err=%v canon=%q err=%v", i, c.Kind, borrowed, errBorrowed, canonical, errCanon)
			}
		}
	}
}
