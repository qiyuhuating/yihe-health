package health

import (
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

func aggregateSleep(samples []*Sample, from, to time.Time) (map[string]float64, float64, float64) {
	if !to.After(from) {
		return map[string]float64{}, 0, 0
	}
	// Compact the private pointer buffer before sorting. A large out-of-window
	// input must remain a linear scan rather than paying for an irrelevant sort.
	kept := samples[:0]
	for _, sample := range samples {
		if sample.Start.Before(to) && sample.End.After(from) && sample.End.After(sample.Start) {
			kept = append(kept, sample)
		}
	}
	samples = kept
	// Clipping start to from preserves chronological order. Equal clipped
	// starts overlap, so their relative order cannot change any union endpoint.
	if !sortSleepNative(samples) {
		slices.SortFunc(samples, func(a, b *Sample) int { return a.Start.Compare(b.Start) })
	}
	stages := map[string]*sleepUnion{}
	var asleep, awake, combined sleepUnion
	for _, sample := range samples {
		start, end := sample.Start, sample.End
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if !end.After(start) {
			continue
		}
		stage := stages[sample.Stage]
		if stage == nil {
			stage = &sleepUnion{}
			stages[sample.Stage] = stage
		}
		stage.add(start, end)
		switch sample.Stage {
		case "asleep", "core", "deep", "rem":
			asleep.add(start, end)
			combined.add(start, end)
		case "awake":
			awake.add(start, end)
			combined.add(start, end)
		}
	}
	minutes := make(map[string]float64, len(stages))
	for name, union := range stages {
		minutes[name] = union.minutes()
	}
	asleepMinutes := asleep.minutes()
	return minutes, asleepMinutes, asleepMinutes + awake.minutes() - combined.minutes()
}
