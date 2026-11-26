package imagecodec

import (
	"bytes"
	"testing"
)

func TestPixelRowsEquivalent(t *testing.T) {
	for _, channels := range []int{3, 4} {
		for width := 0; width <= 65; width++ {
			for offset := 0; offset < 4; offset++ {
				src := make([]byte, width*channels+offset)[offset:]
				for i := range src {
					src[i] = byte(i*37 + width)
				}
				want := make([]byte, width*4)
				dst := bytes.Repeat([]byte{0x5a}, width*4+2)
				got := dst[1 : len(dst)-1]
				if channels == 3 {
					unpackBGR24Scalar(want, src)
					unpackBGR24(got, src)
				} else {
					unpackBGRX32Scalar(want, src)
					unpackBGRX32(got, src)
				}
				if !bytes.Equal(got, want) || dst[0] != 0x5a || dst[len(dst)-1] != 0x5a {
					t.Fatalf("channels=%d width=%d offset=%d", channels, width, offset)
				}
			}
		}
	}
}
