package blob

import (
	"bytes"
	"encoding/base64"
	"testing"

	"yihe-care/internal/securestore"
)

func BenchmarkImageDecode(b *testing.B) {
	box, err := securestore.New(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)))
	if err != nil {
		b.Fatal(err)
	}
	plain := bytes.Repeat([]byte{0x00, 0xff, 0x80, 0x7f}, 256<<10)
	sealed, err := box.SealBytes(plain, "image:benchmark.png")
	if err != nil {
		b.Fatal(err)
	}
	data := []byte(sealed)
	store := &Encrypted{Box: box}
	b.SetBytes(int64(len(plain)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := store.decode("benchmark.png", data)
		if err != nil || len(got) != len(plain) {
			b.Fatal("decode", err)
		}
	}
}
