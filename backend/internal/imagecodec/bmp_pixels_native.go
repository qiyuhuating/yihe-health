//go:build linux && amd64 && cgo && !purego

package imagecodec

/*
#cgo CFLAGS: -O3
#include <stdint.h>
void yihe_unpack_bmp(uint8_t *out, const uint8_t *src, int width, int height, int stride, int channels, int top_down, uint32_t red, uint32_t green, uint32_t blue);
#cgo noescape yihe_unpack_bmp
#cgo nocallback yihe_unpack_bmp
*/
import "C"

import "unsafe"

// decodeBMP validates dimensions and both buffer lengths before calling.
func unpackBMPNative(out, src []byte, width, height, stride, channels int, topDown bool, red, green, blue uint32) bool {
	if width*height < 4096 {
		return false
	}
	var direction C.int
	if topDown {
		direction = 1
	}
	C.yihe_unpack_bmp(
		(*C.uint8_t)(unsafe.Pointer(&out[0])), (*C.uint8_t)(unsafe.Pointer(&src[0])),
		C.int(width), C.int(height), C.int(stride), C.int(channels), direction, C.uint32_t(red), C.uint32_t(green), C.uint32_t(blue),
	)
	return true
}
