package devicecredential

import (
	"strings"
	"testing"
)

func TestCredentialRoundTrip(t *testing.T) {
	clear := strings.Repeat("A", 32)
	hash, err := Hash(clear)
	if err != nil {
		t.Fatal(err)
	}
	if !Matches(clear, hash) {
		t.Fatal("valid device secret rejected")
	}
	if Matches(strings.Repeat("B", 32), hash) {
		t.Fatal("incorrect device secret accepted")
	}
	if Matches(clear, hash+"!") || Matches(clear, "sha256$invalid") {
		t.Fatal("malformed stored hash accepted")
	}
}

func TestCheckDeviceStatus(t *testing.T) {
	clear := strings.Repeat("A", 32)
	hash, err := Hash(clear)
	if err != nil {
		t.Fatal(err)
	}
	enabled, disabled := 1, 0
	if Check(clear, hash, &enabled) != Valid || Check(clear, hash, nil) != Valid {
		t.Fatal("enabled device rejected")
	}
	if Check(clear, hash, &disabled) != Disabled {
		t.Fatal("disabled device accepted")
	}
	if Check(strings.Repeat("B", 32), hash, &disabled) != InvalidSecret {
		t.Fatal("credential mismatch must precede disabled-state disclosure")
	}
}
