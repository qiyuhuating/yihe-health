package userphone

import (
	"encoding/base64"
	"strings"
	"testing"

	"yihe-care/internal/securestore"
)

func testBox(t *testing.T, key byte) *securestore.Box {
	t.Helper()
	box, err := securestore.New(base64.StdEncoding.EncodeToString([]byte(strings.Repeat(string([]byte{key}), 32))))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

func TestProtectAndReveal(t *testing.T) {
	box := testBox(t, 11)
	record, err := Protect(box, 42, "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if record.Ciphertext == "13800138000" || strings.Contains(record.Ciphertext, "13800138000") {
		t.Fatal("plaintext leaked")
	}
	got, err := Reveal(box, 42, record)
	if err != nil || got != "13800138000" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	if _, err := Reveal(box, 43, record); err == nil {
		t.Fatal("ciphertext moved between users")
	}
	record.Lookup = strings.Repeat("0", 64)
	if _, err := Reveal(box, 42, record); err == nil {
		t.Fatal("tampered lookup accepted")
	}
}

func TestLookupKeyIsolation(t *testing.T) {
	first, second := testBox(t, 3), testBox(t, 4)
	a, err := Lookup(first, "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Lookup(second, "13800138000")
	if err != nil || a == b {
		t.Fatal("lookup key did not isolate indexes")
	}
	if _, err := Lookup(nil, "13800138000"); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := Protect(first, 1, "invalid"); err == nil {
		t.Fatal("invalid phone accepted")
	}
}
