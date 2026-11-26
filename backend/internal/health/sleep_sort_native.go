//go:build linux && amd64 && cgo && !purego

package health

/*
#cgo CXXFLAGS: -O3
#cgo LDFLAGS: -lstdc++
#include <stdint.h>
void yihe_sort_sleep(uint64_t *keys, uint32_t count);
#cgo noescape yihe_sort_sleep
#cgo nocallback yihe_sort_sleep
*/
import "C"

import "math/bits"

func sortSleepNative(samples []*Sample) bool {
	if len(samples) < 2048 || uint64(len(samples)) > uint64(^uint32(0)) {
		return false
	}
	// Normal device uploads often arrive in time order. Avoid packing them.
	ordered := true
	previous := samples[0].Start
	if previous != previous.Round(0) {
		return false
	}
	for _, sample := range samples[1:] {
		start := sample.Start
		if start != start.Round(0) {
			return false
		}
		if start.Before(previous) {
			ordered = false
			break
		}
		previous = start
	}
	if ordered {
		return true
	}
	// Keep the original index in the low bits and a signed nanosecond
	// offset in the high bits. The bias maps signed offsets to unsigned
	// order, so C++ can sort the keys with a single integer comparison.
	indexBits := bits.Len(uint(len(samples) - 1))
	indexMask := (C.uint64_t(1) << indexBits) - 1
	limit := int64(1) << (63 - indexBits)
	const minSeconds, maxSeconds = -62167219200, 253402300800
	origin := samples[0].Start.Unix()
	if origin < minSeconds || origin >= maxSeconds {
		return false
	}
	maxDeltaSeconds := limit/1000000000 + 1
	firstSecond := max(int64(minSeconds), origin-maxDeltaSeconds)
	lastSecond := min(int64(maxSeconds-1), origin+maxDeltaSeconds)
	keys := make([]C.uint64_t, len(samples))
	for i, sample := range samples {
		start := sample.Start
		seconds := start.Unix()
		if start != start.Round(0) || seconds < firstSecond || seconds > lastSecond {
			return false
		}
		offset := (seconds-origin)*1000000000 + int64(start.Nanosecond())
		if offset < -limit || offset >= limit {
			return false
		}
		keys[i] = C.uint64_t(uint64(offset+limit)<<indexBits | uint64(i))
	}
	C.yihe_sort_sleep(&keys[0], C.uint32_t(len(keys)))

	// Apply the sorted permutation in place. Replacing each visited index
	// with its own position marks the cycle as complete.
	for i := range keys {
		next := int(keys[i] & indexMask)
		if next == i {
			continue
		}
		saved := samples[i]
		at := i
		for next != i {
			samples[at] = samples[next]
			keys[at] = C.uint64_t(at)
			at = next
			next = int(keys[at] & indexMask)
		}
		samples[at] = saved
		keys[at] = C.uint64_t(at)
	}
	return true
}
