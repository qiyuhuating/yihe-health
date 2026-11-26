//go:build amd64 && !purego && (!linux || !cgo)

package imagecodec

func unpackBGRX32(dst, src []byte) {
	n := len(src) &^ 15
	if n != 0 {
		unpackBGRX32SSE2(dst[:n], src[:n])
	}
	unpackBGRX32Scalar(dst[n:], src[n:])
}

//go:noescape
func unpackBGRX32SSE2(dst, src []byte)

// SSSE3 uses the same OS-managed XMM state as baseline SSE2.
var hasSSSE3 = cpuHasSSSE3()

func unpackBGR24(dst, src []byte) {
	if hasSSSE3 && len(src) >= 16 {
		// A load consumes 12 bytes but reads 16; leave the last bytes to Go.
		n := (len(src) - 4) / 12 * 12
		unpackBGR24SSSE3(dst, src)
		dst, src = dst[n/3*4:], src[n:]
	}
	unpackBGR24Scalar(dst, src)
}

func cpuHasSSSE3() bool

//go:noescape
func unpackBGR24SSSE3(dst, src []byte)
