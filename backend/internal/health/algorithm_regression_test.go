package health

import (
	"testing"
	"time"
)

func reviewStep(at time.Time, v float64) Sample {
	return Sample{Metric: "steps", Source: "watch", SourceDevice: "A", Semantics: "daily_total", Unit: "count", Value: Number(v), Start: at, End: at}
}
func TestAlgorithmMultiDayTotals(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	xs := []Sample{reviewStep(from.Add(time.Hour), 100), reviewStep(from.Add(2*time.Hour), 150), reviewStep(from.Add(25*time.Hour), 200)}
	out, err := Aggregate(xs, from, from.Add(48*time.Hour))
	if err != nil || out.Total == nil || *out.Total != 350 || out.Count != 2 {
		total := -1.0
		if out.Total != nil {
			total = *out.Total
		}
		t.Fatalf("got total=%v count=%v err=%v; want total350 count2", total, out.Count, err)
	}
}
func TestAlgorithmConflictingDailyTimestamp(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	if _, err := Aggregate([]Sample{reviewStep(from.Add(time.Hour), 100), reviewStep(from.Add(time.Hour), 200)}, from, from.Add(24*time.Hour)); err == nil {
		t.Fatal("conflicting totals silently accepted")
	}
}
func TestAlgorithmLegacyTimezone(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := reviewStep(from.Add(time.Hour), 100)
	s.TimeBasis = "legacy-unzoned"
	if out, err := Aggregate([]Sample{s}, from, from.Add(24*time.Hour)); err == nil {
		t.Fatalf("unknown timezone silently accepted as %+v", out)
	}
}
func TestAlgorithmDeletedFirstSample(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := reviewStep(from.Add(time.Hour), 100)
	d := s
	d.Deleted = true
	d.Source = "other"
	out, err := Aggregate([]Sample{d, s}, from, from.Add(24*time.Hour))
	if err != nil || out.Total == nil || *out.Total != 100 {
		t.Fatalf("deleted sample poisoned series: %+v %v", out, err)
	}
}
func TestAlgorithmNilMeasurement(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := reviewStep(from.Add(time.Hour), 100)
	s.Value = nil
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("nil measurement panic: %v", p)
		}
	}()
	out, err := Aggregate([]Sample{s}, from, from.Add(24*time.Hour))
	if err != nil || out.Count != 0 {
		t.Fatalf("want empty statistics got %+v %v", out, err)
	}
}
func TestAlgorithmSplitConservation(t *testing.T) {
	midnight := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	s := reviewStep(midnight.Add(-time.Minute), 1)
	s.Semantics = "interval"
	s.End = midnight.Add(time.Minute)
	a, ea := Aggregate([]Sample{s}, midnight.Add(-24*time.Hour), midnight)
	b, eb := Aggregate([]Sample{s}, midnight, midnight.Add(24*time.Hour))
	whole, ew := Aggregate([]Sample{s}, midnight.Add(-24*time.Hour), midnight.Add(24*time.Hour))
	if ea != nil || eb != nil || ew != nil {
		t.Fatalf("split rejected %v %v %v", ea, eb, ew)
	}
	if *a.Total+*b.Total != *whole.Total {
		t.Fatalf("left=%v right=%v whole=%v: counts are not conserved", *a.Total, *b.Total, *whole.Total)
	}
}
func TestAlgorithmIntervalMinMaxContract(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := reviewStep(from.Add(time.Hour), 100)
	s.Semantics = "interval"
	s.End = s.Start.Add(time.Minute)
	out, err := Aggregate([]Sample{s}, from, from.Add(24*time.Hour))
	if err != nil || out.Min == nil || out.Max == nil {
		t.Fatalf("original interval min/max disappeared: %+v %v", out, err)
	}
}

func TestAlgorithmProratedNominal(t *testing.T) {
	midnight := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	s := reviewStep(midnight.Add(-5*time.Minute), 150)
	s.Semantics = "interval"
	s.End = midnight.Add(10 * time.Minute)
	for _, tc := range []struct {
		from, to time.Time
		want     float64
	}{{midnight.Add(-24 * time.Hour), midnight, 50}, {midnight, midnight.Add(24 * time.Hour), 100}} {
		out, err := Aggregate([]Sample{s}, tc.from, tc.to)
		if err != nil || out.Total == nil || *out.Total != tc.want {
			t.Fatalf("nominal proportional split failed: %+v %v", out, err)
		}
	}
}

func TestIntervalStepsExactLargeCounts(t *testing.T) {
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := reviewStep(start, 9007199254740991)
	s.Semantics = "interval"
	s.End = start.Add(3 * time.Nanosecond)
	for i, want := range []float64{3002399751580330, 3002399751580331, 3002399751580330} {
		got, err := IntervalSteps(s, start.Add(time.Duration(i)), start.Add(time.Duration(i+1)))
		if err != nil || got != want {
			t.Fatalf("part%d got%v err%v want%v", i, got, err, want)
		}
	}
}
func TestIntervalStepsPartitionConservation(t *testing.T) {
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, steps := range []float64{0, 1, 2, 7, 12345, 9007199254740991} {
		s := reviewStep(start, steps)
		s.Semantics = "interval"
		s.End = start.Add(101 * time.Nanosecond)
		sum := 0.0
		for i := 0; i < 101; i++ {
			v, err := IntervalSteps(s, start.Add(time.Duration(i)), start.Add(time.Duration(i+1)))
			if err != nil {
				t.Fatal(err)
			}
			sum += v
		}
		if sum != steps {
			t.Fatalf("101 parts of %v summed to %v", steps, sum)
		}
	}
}
func TestIntervalStepsRejectUnsafeInputs(t *testing.T) {
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	s := reviewStep(start, 1)
	s.Semantics = "interval"
	s.End = start.Add(time.Second)
	for _, value := range []float64{-1, 0.5, 9007199254740992} {
		s.Value = Number(value)
		if _, err := IntervalSteps(s, start, s.End); err == nil {
			t.Fatalf("accepted unsafe value%v", value)
		}
	}
	s.Value = Number(1)
	for _, end := range []time.Time{start, start.Add(-time.Second), start.AddDate(300, 0, 0)} {
		s.End = end
		if _, err := IntervalSteps(s, start, start.Add(time.Second)); err == nil {
			t.Fatalf("accepted invalid/overflowing duration%v", end)
		}
	}
}
func TestIntervalAggregateClippingContract(t *testing.T) {
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	a := reviewStep(start, 10)
	a.Semantics = "interval"
	a.End = start.Add(10 * time.Minute)
	b := a
	b.Start = a.End
	b.End = b.Start.Add(10 * time.Minute)
	b.Value = Number(20)
	out, err := Aggregate([]Sample{b, a}, start.Add(5*time.Minute), start.Add(15*time.Minute))
	if err != nil || out.Count != 2 || out.Total == nil || *out.Total != 15 || out.Min == nil || *out.Min != 5 || out.Max == nil || *out.Max != 10 {
		t.Fatalf("wrong clipped stats %+v %v", out, err)
	}
	if *a.Value != 10 || *b.Value != 20 {
		t.Fatal("mutated caller values")
	}
	b.Start = start.Add(9 * time.Minute)
	if _, err := Aggregate([]Sample{a, b}, start, start.Add(15*time.Minute)); err == nil {
		t.Fatal("accepted overlapping crossing intervals")
	}
}
