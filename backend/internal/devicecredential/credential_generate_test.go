package devicecredential

import (
	"errors"
	"strings"
	"testing"
)

func TestGenerateProducesMatchingUniqueCredentials(t *testing.T) {
	clearA, hashA, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	clearB, hashB, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(clearA) != 32 || strings.TrimSpace(clearA) != clearA {
		t.Fatalf("unexpected credential shape %q", clearA)
	}
	if clearA == clearB || hashA == hashB {
		t.Fatal("two generated credentials collided")
	}
	if !Matches(clearA, hashA) || Matches(clearA, hashB) {
		t.Fatal("generated credential does not match exactly its own hash")
	}
	if strings.Contains(hashA, clearA) {
		t.Fatal("stored hash must not contain the cleartext secret")
	}
}

func TestSecretLengthBoundaries(t *testing.T) {
	validStored, err := Hash(strings.Repeat("A", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		length int
		ok     bool
	}{{0, false}, {23, false}, {24, true}, {64, true}, {65, false}} {
		clear := strings.Repeat("A", tc.length)
		hash, err := Hash(clear)
		if tc.ok {
			if err != nil || !Matches(clear, hash) {
				t.Fatalf("length %d should be accepted, got err=%v", tc.length, err)
			}
			continue
		}
		if !errors.Is(err, ErrSecretLength) {
			t.Fatalf("length %d should return ErrSecretLength, got %v", tc.length, err)
		}
		if Matches(clear, validStored) {
			t.Fatalf("length %d supplied secret must never match", tc.length)
		}
	}
}

func TestMatchesRejectsWrongStoredFormat(t *testing.T) {
	clear := strings.Repeat("A", 32)
	hash, err := Hash(clear)
	if err != nil {
		t.Fatal(err)
	}
	for name, stored := range map[string]string{
		"empty":        "",
		"wrong prefix": "sha512$" + strings.TrimPrefix(hash, hashPrefix),
		"short":        hash[:len(hash)-1],
		"bad base64":   hashPrefix + strings.Repeat("!", 43),
	} {
		if Matches(clear, stored) {
			t.Fatalf("%s stored value was accepted", name)
		}
	}
}
