package securestore

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestSealBytesRoundTrip(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	legacy, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewKeyring("active", map[string]string{"active": key}, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, box := range []*Box{legacy, rotated} {
		for _, payload := range [][]byte{nil, {0, 255, 1}, bytes.Repeat([]byte("health"), 128)} {
			sealed, err := box.SealBytes(payload, "scope")
			if err != nil {
				t.Fatal(err)
			}
			plain, err := box.Open(sealed, "scope")
			if err != nil || !bytes.Equal([]byte(plain), payload) {
				t.Fatal("round trip mismatch", err)
			}
			if _, err := box.Open(sealed, "other"); err == nil {
				t.Fatal("scope mismatch accepted")
			}
		}
	}
}

func BenchmarkSealHealthPayload(b *testing.B) {
	box, err := New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte("health"), 64)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for range b.N {
		if _, err := box.SealBytes(payload, "health-change:12345:1:synthetic"); err != nil {
			b.Fatal(err)
		}
	}
}
