package securestore

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestAuthenticatedEnvelope(t *testing.T) {
	b, err := New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := b.Seal("医疗与复核记录", "note")
	c, _ := b.Seal("医疗与复核记录", "note")
	if a == c || strings.Contains(a, "医疗") {
		t.Fatal("envelope is not randomized")
	}
	if got, err := b.Open(a, "note"); err != nil || got != "医疗与复核记录" {
		t.Fatal(got, err)
	}
	if _, err = b.Open(a, "session"); err == nil {
		t.Fatal("scope substitution accepted")
	}
	// Raw base64 can otherwise admit alternate text for the same trailing byte.
	// Require canonical encoding and separately flip a real ciphertext byte.
	if _, err = b.Open(a[:len(a)-2]+"xx", "note"); err == nil {
		t.Fatal("non-canonical envelope encoding accepted")
	}
	encoded := strings.TrimPrefix(a, Prefix)
	payload, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)-1] ^= 1
	tampered := Prefix + base64.RawStdEncoding.EncodeToString(payload)
	if _, err = b.Open(tampered, "note"); err == nil {
		t.Fatal("ciphertext tampering accepted")
	}
}
