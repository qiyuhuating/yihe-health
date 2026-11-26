package imagecodec

import (
	"encoding/binary"
	"fmt"
	"testing"
)

func BenchmarkDecodeBMP(b *testing.B) {
	const width, height = 1920, 1080
	for _, channels := range []int{3, 4} {
		b.Run(fmt.Sprintf("BGR%d", channels*8), func(b *testing.B) {
			stride := (width*channels + 3) &^ 3
			data := make([]byte, 54+stride*height)
			copy(data, "BM")
			binary.LittleEndian.PutUint32(data[10:], 54)
			binary.LittleEndian.PutUint32(data[14:], 40)
			binary.LittleEndian.PutUint32(data[18:], width)
			binary.LittleEndian.PutUint32(data[22:], height)
			binary.LittleEndian.PutUint16(data[26:], 1)
			binary.LittleEndian.PutUint16(data[28:], uint16(8*channels))
			for i := 54; i < len(data); i++ {
				data[i] = byte(i * 17)
			}
			b.SetBytes(int64(stride * height))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, _, ok := decodeBMP(data); !ok {
					b.Fatal("decode failed")
				}
			}
		})
	}
}
