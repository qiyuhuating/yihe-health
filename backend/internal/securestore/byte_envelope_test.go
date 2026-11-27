package securestore

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestByteEnvelopeInteroperability(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	next := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	id := strings.Repeat("k", 32)
	legacy, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewKeyring(id, map[string]string{"old": key, id: next}, key)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0, 0xff, 0x80, 0x7f}, 1024)
	for _, writer := range []*Box{legacy, rotated} {
		text, err := writer.SealBytes(payload, "image:test")
		if err != nil {
			t.Fatal(err)
		}
		got, err := rotated.OpenEncodedBytes([]byte(text), "image:test")
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("text to bytes: %v", err)
		}
		encoded, err := writer.SealEncodedBytes(payload, "image:test")
		if err != nil {
			t.Fatal(err)
		}
		original := bytes.Clone(encoded)
		got, err = rotated.OpenBytes(string(encoded), "image:test")
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("bytes to text: %v", err)
		}
		got, err = rotated.OpenEncodedBytes(encoded, "image:test")
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("bytes to bytes: %v", err)
		}
		got[0] ^= 1
		if !bytes.Equal(encoded, original) {
			t.Fatal("input modified or aliased")
		}
		if got, err := rotated.OpenEncodedBytes(encoded, "image:wrong"); err == nil || got != nil {
			t.Fatal("incorrect scope accepted")
		}
		encoded[len(encoded)-2] ^= 1
		if got, err := rotated.OpenEncodedBytes(encoded, "image:test"); err == nil || got != nil {
			t.Fatal("corrupted envelope accepted")
		}
	}
}

func BenchmarkSealImageEnvelope(b *testing.B) {
	box, err := New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	payload := make([]byte, 1<<20)
	for _, native := range []bool{false, true} {
		name := "text-roundtrip"
		if native {
			name = "native-bytes"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			for b.Loop() {
				if native {
					benchmarkOpenedPayload, err = box.SealEncodedBytes(payload, "image:benchmark")
				} else {
					var text string
					text, err = box.SealBytes(payload, "image:benchmark")
					benchmarkOpenedPayload = []byte(text)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
