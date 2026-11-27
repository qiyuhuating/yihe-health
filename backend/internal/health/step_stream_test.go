package health

import (
	"fmt"
	"math"
	"math/bits"
	"math/rand"
	"reflect"
	"slices"
	"sort"
	"testing"
	"time"
)

func TestStepWindowFastPathDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(20261126))
	start := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 10000; i++ {
		duration := time.Duration(rng.Int63n(int64(24*time.Hour)) + 1)
		s := reviewStep(start, float64(rng.Uint64()&9007199254740991))
		s.Semantics = "interval"
		s.End = start.Add(duration)
		from, to := start.Add(-time.Second), s.End.Add(time.Second)
		switch i % 4 {
		case 1:
			from = start.Add(time.Duration(rng.Int63n(int64(duration))))
		case 2:
			to = start.Add(time.Duration(rng.Int63n(int64(duration))))
		case 3:
			from = s.End
			to = s.End.Add(time.Minute)
		}
		want, e1 := intervalStepsOriginal(s, from, to)
		got, e2 := IntervalSteps(s, from, to)
		if want != got || (e1 == nil) != (e2 == nil) {
			t.Fatalf("case %d got=%v %v want=%v %v", i, got, e2, want, e1)
		}
	}
}

func TestStepSeriesOrderedAndUnordered(t *testing.T) {
	rng := rand.New(rand.NewSource(211))
	start := time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	for trial := range 120 {
		samples := make([]Sample, 128)
		for i := range samples {
			s := reviewStep(start.Add(time.Duration(i)*time.Minute), float64(i))
			s.Semantics = "interval"
			s.End = s.Start.Add(time.Minute)
			s.Deleted = i%17 == 0
			samples[i] = s
		}
		switch trial % 4 {
		case 1:
			rng.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
		case 2:
			samples[20].End = samples[20].End.Add(time.Second)
		case 3:
			samples[21].Source = "other"
		}
		before := slices.Clone(samples)
		filtered := make([]*Sample, 0, len(samples))
		var want Statistics
		var err error
		for i := range samples {
			s := &samples[i]
			if s.Deleted || s.Value == nil {
				continue
			}
			if len(filtered) > 0 {
				if err = validateSeriesSample(filtered[0], s); err != nil {
					break
				}
			}
			filtered = append(filtered, s)
		}
		from, to := start.Add(90*time.Second), start.Add(120*time.Minute+time.Second)
		if err == nil {
			want, err = aggregateStepIntervalsOriginal(filtered, Statistics{Unit: filtered[0].Unit}, from, to)
		}
		got, e := Aggregate(samples, from, to)
		if (e == nil) != (err == nil) || (e != nil && e.Error() != err.Error()) || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(samples, before) {
			t.Fatalf("trial=%d got=%+v %v want=%+v %v", trial, got, e, want, err)
		}
	}
}
func intervalStepsOriginal(s Sample, from, to time.Time) (float64, error) {
	if s.Value == nil || !(*s.Value >= 0 && *s.Value <= 9007199254740991) || math.Trunc(*s.Value) != *s.Value {
		return 0, fmt.Errorf("step count must be an exact nonnegative integer")
	}
	duration := s.End.Sub(s.Start)
	if duration <= 0 || !s.Start.Add(duration).Equal(s.End) || !to.After(from) {
		return 0, fmt.Errorf("invalid step interval or reporting window")
	}
	a, b := from, to
	if a.Before(s.Start) {
		a = s.Start
	}
	if b.After(s.End) {
		b = s.End
	}
	if !b.After(a) {
		return 0, nil
	}
	allocated := func(at time.Time) uint64 {
		// A 128-bit product avoids overflow and floating-point rounding near
		// half steps, including counts close to the exact integer limit.
		hi, lo := bits.Mul64(uint64(*s.Value), uint64(at.Sub(s.Start)))
		q, r := bits.Div64(hi, lo, uint64(duration))
		if r >= uint64(duration)-r {
			q++
		}
		return q
	}
	return float64(allocated(b) - allocated(a)), nil
}

func aggregateStepIntervalsOriginal(filtered []*Sample, out Statistics, from, to time.Time) (Statistics, error) {
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Start.Before(filtered[j].Start) })
	total, low, high := 0.0, math.Inf(1), math.Inf(-1)
	out.Count = 0
	for i, s := range filtered {
		if i > 0 && s.Start.Before(filtered[i-1].End) {
			return out, fmt.Errorf("overlapping step intervals require source review")
		}
		v, err := intervalStepsOriginal(*s, from, to)
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
