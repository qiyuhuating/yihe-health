//go:build !linux || !amd64 || !cgo || purego

package health

func sortSleepNative(_ []*Sample) bool { return false }
