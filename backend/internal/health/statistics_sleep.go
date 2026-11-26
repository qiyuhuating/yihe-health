package health

import (
	"math/bits"
	"slices"
	"time"
)

// Each union consumes intervals in start order. No interval buffers are needed
// after sorting the caller-private sample pointers.
type sleepUnion struct {
	start, end  time.Time
	total       float64
	initialized bool
}

func (u *sleepUnion) add(start, end time.Time) {
	if !u.initialized {
		u.start, u.end, u.initialized = start, end, true
	} else if !start.After(u.end) {
		if end.After(u.end) {
			u.end = end
		}
	} else {
		u.total += u.end.Sub(u.start).Minutes()
		u.start, u.end = start, end
	}
}

func (u *sleepUnion) minutes() float64 {
	if !u.initialized {
		return 0
	}
	return u.total + u.end.Sub(u.start).Minutes()
}

// Validate in original input order, counting even intervals outside the
// window. Only unordered, contributing intervals need a temporary pointer list.
func aggregateSleepSeries(samples []Sample, from, to time.Time) (Statistics, error) {
	var out Statistics
	first := &samples[0]
	var previous *Sample
	ordered, kept := true, 0
	const minSeconds, maxSeconds = -62167219200, 253402300800
	origin, last := int64(maxSeconds), int64(minSeconds)
	wallClock := true
	for i := range samples {
		s := &samples[i]
		if s.Deleted || s.Value == nil {
			continue
		}
		if err := validateSeriesSample(first, s); err != nil {
			return Statistics{}, err
		}
		out.Count++
		if to.After(from) && sleepContributes(s, from, to) {
			ordered = ordered && (previous == nil || !s.Start.Before(previous.Start))
			previous = s
			kept++
			seconds := s.Start.Unix()
			wallClock = wallClock && s.Start == s.Start.Round(0) && seconds >= minSeconds && seconds < maxSeconds
			origin, last = min(origin, seconds), max(last, seconds)
		}
	}
	out.Unit = first.Unit
	if kept == 0 {
		out.StageMinutes = map[string]float64{}
		return out, nil
	}
	if ordered && wallClock {
		var acc sleepAccumulator
		for i := range samples {
			s := &samples[i]
			if !s.Deleted && s.Value != nil && sleepContributes(s, from, to) {
				acc.add(s, from, to)
			}
		}
		out.StageMinutes, out.AsleepMinutes, out.ConflictingSleepMinutes = acc.result()
		return out, nil
	}
	// Keep only packed timestamp/index keys. The source samples stay in
	// place, and sorted indices feed the union directly without a pointer
	// array or permutation. Very wide and monotonic-clock inputs use the
	// original comparison sort.
	if wallClock && kept >= 2048 && uint64(len(samples)) <= uint64(^uint32(0)) {
		indexBits := bits.Len(uint(len(samples) - 1))
		limit := ^uint64(0) >> indexBits
		if uint64(last-origin) < limit/1000000000 {
			keys := make([]uint64, 0, kept)
			for i := range samples {
				s := &samples[i]
				if !s.Deleted && s.Value != nil && sleepContributes(s, from, to) {
					offset := uint64(s.Start.Unix()-origin)*1000000000 + uint64(s.Start.Nanosecond())
					keys = append(keys, offset<<indexBits|uint64(i))
				}
			}
			sortSleepKeys(keys)
			mask := (uint64(1) << indexBits) - 1
			var acc sleepAccumulator
			for _, key := range keys {
				acc.add(&samples[int(key&mask)], from, to)
			}
			out.StageMinutes, out.AsleepMinutes, out.ConflictingSleepMinutes = acc.result()
			return out, nil
		}
	}
	filtered := make([]*Sample, 0, kept)
	for i := range samples {
		s := &samples[i]
		if !s.Deleted && s.Value != nil && sleepContributes(s, from, to) {
			filtered = append(filtered, s)
		}
	}
	sortSleep(filtered)
	var acc sleepAccumulator
	for _, s := range filtered {
		acc.add(s, from, to)
	}
	out.StageMinutes, out.AsleepMinutes, out.ConflictingSleepMinutes = acc.result()
	return out, nil
}

func sleepContributes(s *Sample, from, to time.Time) bool {
	return s.Start.Before(to) && s.End.After(from) && s.End.After(s.Start)
}

func sortSleep(samples []*Sample) {
	if !sortSleepNative(samples) {
		slices.SortFunc(samples, func(a, b *Sample) int { return a.Start.Compare(b.Start) })
	}
}

func aggregateSleep(samples []*Sample, from, to time.Time) (map[string]float64, float64, float64) {
	if !to.After(from) {
		return map[string]float64{}, 0, 0
	}
	// Compact the private pointer buffer before sorting. A large out-of-window
	// input must remain a linear scan rather than paying for an irrelevant sort.
	kept := samples[:0]
	for _, sample := range samples {
		if sleepContributes(sample, from, to) {
			kept = append(kept, sample)
		}
	}
	samples = kept
	// Clipping start to from preserves chronological order. Equal clipped
	// starts overlap, so their relative order cannot change any union endpoint.
	sortSleep(samples)
	var acc sleepAccumulator
	for _, sample := range samples {
		acc.add(sample, from, to)
	}
	return acc.result()
}

var sleepStageNames = [...]string{"asleep", "core", "deep", "rem", "awake", "in_bed"}

type sleepAccumulator struct {
	stages                  [len(sleepStageNames)]sleepUnion
	seen                    uint8
	extra                   map[string]*sleepUnion
	asleep, awake, combined sleepUnion
}

func (a *sleepAccumulator) add(sample *Sample, from, to time.Time) {
	start, end := sample.Start, sample.End
	if start.Before(from) {
		start = from
	}
	if end.After(to) {
		end = to
	}
	if !end.After(start) {
		return
	}
	var stage int
	switch sample.Stage {
	case "asleep":
		stage = 0
	case "core":
		stage = 1
	case "deep":
		stage = 2
	case "rem":
		stage = 3
	case "awake":
		stage = 4
	case "in_bed":
		stage = 5
	default:
		union := a.extra[sample.Stage]
		if union == nil {
			if a.extra == nil {
				a.extra = make(map[string]*sleepUnion)
			}
			union = &sleepUnion{}
			a.extra[sample.Stage] = union
		}
		union.add(start, end)
		return
	}
	a.seen |= 1 << stage
	a.stages[stage].add(start, end)
	if stage < 4 {
		a.asleep.add(start, end)
		a.combined.add(start, end)
	} else if stage == 4 {
		a.awake.add(start, end)
		a.combined.add(start, end)
	}
}

func (a *sleepAccumulator) result() (map[string]float64, float64, float64) {
	minutes := make(map[string]float64, bits.OnesCount8(a.seen)+len(a.extra))
	for mask := a.seen; mask != 0; mask &= mask - 1 {
		i := bits.TrailingZeros8(mask)
		minutes[sleepStageNames[i]] = a.stages[i].minutes()
	}
	for name, union := range a.extra {
		minutes[name] = union.minutes()
	}
	asleepMinutes := a.asleep.minutes()
	return minutes, asleepMinutes, asleepMinutes + a.awake.minutes() - a.combined.minutes()
}
