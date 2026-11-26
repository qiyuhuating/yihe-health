package imagecodec

import (
	"encoding/binary"
	"math/bits"
)

// Both slices contain exactly the same number of four-byte pixels.
func unpackBGRX32Scalar(dst, src []byte) {
	for len(src) != 0 {
		px := binary.LittleEndian.Uint32(src)
		binary.LittleEndian.PutUint32(dst, bits.ReverseBytes32(px<<8)|0xff000000)
		src, dst = src[4:], dst[4:]
	}
}

func unpackBGR24Scalar(dst, src []byte) {
	for len(src) != 0 {
		binary.LittleEndian.PutUint32(dst, uint32(src[2])|uint32(src[1])<<8|uint32(src[0])<<16|0xff000000)
		src, dst = src[3:], dst[4:]
	}
}
