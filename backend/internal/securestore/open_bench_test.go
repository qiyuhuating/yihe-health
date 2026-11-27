package securestore

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestOpenBytesAuthenticatedAndOwned(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	other := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	legacy, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := NewKeyring("new", map[string]string{"old": key, "new": other}, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, writer := range []*Box{legacy, rotated} {
		for _, payload := range [][]byte{nil, {0, 255, 1}, bytes.Repeat([]byte("health"), 128)} {
			sealed, err := writer.SealBytes(payload, "scope")
			if err != nil {
				t.Fatal(err)
			}
			first, err := rotated.OpenBytes(sealed, "scope")
			if err != nil || !bytes.Equal(first, payload) {
				t.Fatal("roundtrip", err)
			}
			if len(first) > 0 {
				first[0] ^= 255
			}
			again, err := rotated.OpenBytes(sealed, "scope")
			if err != nil || !bytes.Equal(again, payload) {
				t.Fatal("plaintext alias", err)
			}
			if plain, err := rotated.OpenBytes(sealed, "wrong"); err == nil || plain != nil {
				t.Fatal("authentication failure returned plaintext")
			}
		}
	}
}

var benchmarkOpenedPayload []byte

func BenchmarkOpenHealthPayload(b *testing.B) {
	box, err := New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte("health"), 64)
	sealed, err := box.SealBytes(payload, "health-sample:synthetic")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for range b.N {
		plain, err := box.OpenBytes(sealed, "health-sample:synthetic")
		if err != nil || len(plain) != len(payload) {
			b.Fatal(err)
		}
		benchmarkOpenedPayload = plain
	}
}
