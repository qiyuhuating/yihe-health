//go:build !linux || !amd64 || !cgo || purego

package imagecodec

func unpackBMPNative(_, _ []byte, _, _, _, _ int, _ bool, _, _, _ uint32) bool { return false }
