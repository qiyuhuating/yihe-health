//go:build !linux || !amd64 || !cgo || purego

package health

import "slices"

func sortSleepNative(_ []*Sample) bool { return false }

func sortSleepKeys(keys []uint64) { slices.Sort(keys) }
