package health

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
	"time"
)

type Statistics struct {
	Count                   int                `json:"count"`
	Unit                    string             `json:"unit"`
	Total                   *float64           `json:"total,omitempty"`
	Min                     *float64           `json:"min,omitempty"`
	Max                     *float64           `json:"max,omitempty"`
	Mean                    *float64           `json:"mean,omitempty"`
	AsleepMinutes           float64            `json:"asleepMinutes,omitempty"`
	ConflictingSleepMinutes float64            `json:"conflictingSleepMinutes,omitempty"`
	StageMinutes            map[string]float64 `json:"stageMinutes,omitempty"`
}

// IntervalSteps allocates an integer step count to [from, to). Rounding the
// cumulative count at each endpoint makes adjacent windows additive. Counts
// must fit the exact integer range of the JSON/float64 sampling contract.
func IntervalSteps(s Sample, from, to time.Time) (float64, error) {
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
	count := uint64(*s.Value)
	if a.Equal(s.Start) && b.Equal(s.End) {
		return float64(count), nil
	}
	allocated := func(at time.Time) uint64 {
		if at.Equal(s.Start) {
			return 0
		}
		if at.Equal(s.End) {
			return count
		}
		// A 128-bit product avoids overflow and floating-point rounding near
		// half steps, including counts close to the exact integer limit.
		hi, lo := bits.Mul64(count, uint64(at.Sub(s.Start)))
		q, r := bits.Div64(hi, lo, uint64(duration))
		if r >= uint64(duration)-r {
			q++
		}
		return q
	}
	return float64(allocated(b) - allocated(a)), nil
}

// Aggregate receives one explicit source, device, metric and semantics at a time.
// Sleep intervals are unioned; step intervals must not overlap. Daily snapshots
// use only the latest sample per day in the requested reporting offset.
func Aggregate(samples []Sample, from, to time.Time) (Statistics, error) {
	// Scalar series need neither sorting nor a temporary pointer for each
	// sample. Keep the original input order and floating-point operations.
	firstIndex := -1
	for i := range samples {
		if !samples[i].Deleted && samples[i].Value != nil {
			firstIndex = i
			break
		}
	}
	if firstIndex < 0 {
		return Statistics{}, nil
	}
	// An ignored prefix cannot affect validation, units or sampling identity.
	samples = samples[firstIndex:]
	s := &samples[0]
	if s.Semantics != "daily_total" && s.Metric != "sleep_stage" && !(s.Metric == "steps" && s.Semantics == "interval") {
		return aggregateScalars(samples, s)
	}
	if s.Semantics != "daily_total" && s.Metric == "sleep_stage" {
		return aggregateSleepSeries(samples, from, to)
	}
	if s.Metric == "steps" && s.Semantics == "interval" {
		return aggregateStepSeries(samples, s, from, to)
	}
	out := Statistics{}
	type calendarDay struct {
		year  int
		month time.Month
		day   int
	}
	daily := map[calendarDay]*Sample{}
	// Ordered observations only need the preceding timestamp. On the first
	// reversal, reconstruct the already validated prefix in a compact map.
	type instant struct {
		seconds int64
		nanos   int32
	}
	keyAt := func(t time.Time) instant { return instant{t.Unix(), int32(t.Nanosecond())} }
	var dailyObserved map[instant]float64
	var previous *Sample
	observed := 0
	filtered := []*Sample{}
	first := &samples[0]
	for i := range samples {
		s := &samples[i]
		if s.Deleted || s.Value == nil {
			continue
		}
		if err := validateSeriesSample(first, s); err != nil {
			return out, err
		}
		if s.Semantics == "daily_total" {
			at := s.Start.UTC()
			if dailyObserved == nil && previous != nil && at.Before(previous.Start.UTC()) {
				live := observed + 1
				for j := i + 1; j < len(samples); j++ {
					if !samples[j].Deleted && samples[j].Value != nil {
						live++
					}
				}
				dailyObserved = make(map[instant]float64, min(live, 100000))
				for j := 0; j < i; j++ {
					prior := &samples[j]
					if !prior.Deleted && prior.Value != nil {
						dailyObserved[keyAt(prior.Start)] = *prior.Value
					}
				}
			}
			if dailyObserved != nil {
				key := keyAt(at)
				if value, exists := dailyObserved[key]; exists && value != *s.Value {
					return out, fmt.Errorf("conflicting daily totals at the same observation time")
				}
				dailyObserved[key] = *s.Value
			} else if previous != nil && at.Equal(previous.Start.UTC()) && *previous.Value != *s.Value {
				return out, fmt.Errorf("conflicting daily totals at the same observation time")
			}
			previous = s
			observed++
			year, month, day := s.Start.In(from.Location()).Date()
			key := calendarDay{year, month, day}
			if old, ok := daily[key]; !ok || s.Start.After(old.Start) {
				daily[key] = s
			}
		}
		if s.Semantics != "daily_total" {
			filtered = append(filtered, s)
		}
	}
	out.Unit = first.Unit
	if first.Semantics == "daily_total" {
		// Only one pointer per reporting day is needed, independent of the
		// number of cumulative observations received during that day.
		filtered = make([]*Sample, 0, len(daily))
		for _, s := range daily {
			filtered = append(filtered, s)
		}
	}
	out.Count = len(filtered)
	if first.Metric == "sleep_stage" {
		out.StageMinutes, out.AsleepMinutes, out.ConflictingSleepMinutes = aggregateSleep(filtered, from, to)
		return out, nil
	}

	return aggregateTotals(filtered, first, out)
}

// SQL supplies ordered windows. Validate the entire series first to preserve
// error precedence, then aggregate directly without materializing pointers.
// Unordered imports retain the original sort-based path.
func aggregateStepSeries(samples []Sample, first *Sample, from, to time.Time) (Statistics, error) {
	var previous *Sample
	ordered := true
	count := 0
	for i := range samples {
		s := &samples[i]
		if s.Deleted || s.Value == nil {
			continue
		}
		if err := validateSeriesSample(first, s); err != nil {
			return Statistics{}, err
		}
		if previous != nil && s.Start.Before(previous.Start) {
			ordered = false
		}
		previous = s
		count++
	}
	out := Statistics{Unit: first.Unit}
	if !ordered {
		filtered := make([]*Sample, 0, count)
		for i := range samples {
			s := &samples[i]
			if !s.Deleted && s.Value != nil {
				filtered = append(filtered, s)
			}
		}
		return aggregateStepIntervals(filtered, out, from, to)
	}
	var acc stepAccumulator
	acc.low, acc.high = math.Inf(1), math.Inf(-1)
	for i := range samples {
		s := &samples[i]
		if s.Deleted || s.Value == nil {
			continue
		}
		if err := acc.add(s, from, to); err != nil {
			out.Count = acc.count
			return out, err
		}
	}
	return acc.result(out), nil
}

type stepAccumulator struct {
	previous         *Sample
	total, low, high float64
	count            int
}

func (a *stepAccumulator) add(s *Sample, from, to time.Time) error {
	if a.previous != nil && s.Start.Before(a.previous.End) {
		return fmt.Errorf("overlapping step intervals require source review")
	}
	a.previous = s
	v, err := IntervalSteps(*s, from, to)
	if err != nil {
		return err
	}
	if !s.End.After(from) || !s.Start.Before(to) {
		return nil
	}
	a.total += v
	if a.total > 9007199254740991 {
		return fmt.Errorf("step total exceeds exact integer range")
	}
	a.low, a.high = math.Min(a.low, v), math.Max(a.high, v)
	a.count++
	return nil
}

func (a *stepAccumulator) result(out Statistics) Statistics {
	out.Count = a.count
	if a.count > 0 {
		out.Total, out.Min, out.Max = Number(a.total), Number(a.low), Number(a.high)
	}
	return out
}

func aggregateStepIntervals(filtered []*Sample, out Statistics, from, to time.Time) (Statistics, error) {
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Start.Before(filtered[j].Start) })
	acc := stepAccumulator{low: math.Inf(1), high: math.Inf(-1)}
	for _, s := range filtered {
		if err := acc.add(s, from, to); err != nil {
			out.Count = acc.count
			return out, err
		}
	}
	return acc.result(out), nil
}

func aggregateTotals(filtered []*Sample, first *Sample, out Statistics) (Statistics, error) {
	total, low, high := 0.0, *filtered[0].Value, *filtered[0].Value
	for _, s := range filtered {
		v := *s.Value
		total += v
		if math.IsInf(total, 0) {
			return out, fmt.Errorf("measurement total exceeds finite range")
		}
		if first.Metric == "steps" && total > 9007199254740991 {
			return out, fmt.Errorf("step total exceeds exact integer range")
		}
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
	}
	out.Min = Number(low)
	out.Max = Number(high)
	if first.Semantics == "instant" {
		out.Mean = Number(total / float64(out.Count))
	} else {
		out.Total = Number(total)
	}
	return out, nil
}

func validateSeriesSample(first, sample *Sample) error {
	if sample.TimeBasis == "legacy-unzoned" {
		return fmt.Errorf("legacy time zone is unknown; use the legacy daily views until reviewed")
	}
	if first.Metric != sample.Metric || first.Source != sample.Source || first.SourceDevice != sample.SourceDevice || first.Semantics != sample.Semantics {
		return fmt.Errorf("mixed sampling sources or semantics")
	}
	return nil
}

func aggregateScalars(samples []Sample, first *Sample) (Statistics, error) {
	count := 0
	total, low, high := 0.0, *first.Value, *first.Value
	for i := range samples {
		s := &samples[i]
		if s.Deleted || s.Value == nil {
			continue
		}
		if err := validateSeriesSample(first, s); err != nil {
			return Statistics{}, err
		}
		v := *s.Value
		total += v
		if v < low {
			low = v
		}
		if v > high {
			high = v
		}
		count++
	}
	out := Statistics{Count: count, Unit: first.Unit, Min: Number(low), Max: Number(high)}
	if first.Semantics == "instant" {
		out.Mean = Number(total / float64(count))
		if math.IsInf(total, 1) && !math.IsInf(high, 0) && low >= 0 {
			// Finite nonnegative samples always have a finite mean. Scale only
			// the overflowing case, preserving the ordinary arithmetic path.
			scaled := 0.0
			for i := range samples {
				if !samples[i].Deleted && samples[i].Value != nil {
					scaled += *samples[i].Value / high
				}
			}
			out.Mean = Number((scaled / float64(count)) * high)
		}
	} else {
		out.Total = Number(total)
	}
	return out, nil
}
