package securestore

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestOpenBytesReuseAuthenticationAndRotation(t *testing.T) {
	old := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	next := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	legacy, err := New(old)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewKeyring("next", map[string]string{"old": old, "next": next}, old)
	if err != nil {
		t.Fatal(err)
	}
	var scratch []byte
	for _, writer := range []*Box{legacy, rotated} {
		for _, size := range []int{0, 384, 17, 768, 1} {
			want := bytes.Repeat([]byte{byte(size)}, size)
			cipher, err := writer.SealBytes(want, "sample")
			if err != nil {
				t.Fatal(err)
			}
			if got, err := rotated.OpenBytesReuse(cipher, "incorrect", &scratch); err == nil || got != nil {
				t.Fatal("wrong scope accepted")
			}
			if got, err := rotated.OpenBytesReuse(cipher+"!", "sample", &scratch); err == nil || got != nil {
				t.Fatal("invalid encoding accepted")
			}
			got, err := rotated.OpenBytesReuse(cipher, "sample", &scratch)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("size %d: %v", size, err)
			}
		}
	}
}

func BenchmarkOpenHealthPayloadReuse(b *testing.B) {
	box, err := New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte("health"), 64)
	cipher, err := box.SealBytes(payload, "health-sample:synthetic")
	if err != nil {
		b.Fatal(err)
	}
	for _, reuse := range []bool{false, true} {
		name := "fresh"
		if reuse {
			name = "reuse"
		}
		b.Run(name, func(b *testing.B) {
			var scratch []byte
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for range b.N {
				var plain []byte
				var err error
				if reuse {
					plain, err = box.OpenBytesReuse(cipher, "health-sample:synthetic", &scratch)
				} else {
					plain, err = box.OpenBytes(cipher, "health-sample:synthetic")
				}
				if err != nil || len(plain) != len(payload) {
					b.Fatal(err)
				}
				benchmarkOpenedPayload = plain
			}
		})
	}
}
