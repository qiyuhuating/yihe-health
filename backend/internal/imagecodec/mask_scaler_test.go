package imagecodec

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"
)

func TestMaskScalerDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(211))
	masks := []uint32{0, 1, 0x1f, 0x7e0, 0xf800, 0xff0000, 0x3ff00000, 0x80000001, 0xffffffff}
	for range 1000 {
		masks = append(masks, rng.Uint32())
	}
	for _, mask := range masks {
		s := newMaskScaler(mask)
		for range 1000 {
			pixel := rng.Uint32()
			if got, want := s.value(pixel), scaleMask(pixel, mask); got != want {
				t.Fatalf("mask=%x pixel=%x got=%d want=%d", mask, pixel, got, want)
			}
		}
	}
}

func BenchmarkBitfieldDecode(b *testing.B) {
	for _, width := range []int{64, 1920} {
		height := width * 9 / 16
		for _, tc := range []struct {
			name  string
			masks [3]uint32
		}{
			{"rgb565", [3]uint32{0xf800, 0x7e0, 0x1f}},
			{"rgb10", [3]uint32{0x3ff00000, 0xffc00, 0x3ff}},
			{"sparse", [3]uint32{0x80000001, 0, 0x7ffffffe}},
		} {
			data := make([]byte, 66+width*height*4)
			copy(data, "BM")
			binary.LittleEndian.PutUint32(data[10:], 66)
			binary.LittleEndian.PutUint32(data[14:], 52)
			binary.LittleEndian.PutUint32(data[18:], uint32(width))
			binary.LittleEndian.PutUint32(data[22:], uint32(height))
			binary.LittleEndian.PutUint16(data[26:], 1)
			binary.LittleEndian.PutUint16(data[28:], 32)
			binary.LittleEndian.PutUint32(data[30:], 3)
			for i, mask := range tc.masks {
				binary.LittleEndian.PutUint32(data[54+i*4:], mask)
			}
			for i := range width * height {
				binary.LittleEndian.PutUint32(data[66+i*4:], uint32(i)*2654435761)
			}
			b.Run(fmt.Sprintf("%s/%d", tc.name, width), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(width * height * 4))
				for range b.N {
					if _, _, _, ok := decodeBMP(data); !ok {
						b.Fatal("decode")
					}
				}
			})
		}
	}
}
