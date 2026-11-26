package securecode

import (
	"errors"
	"strings"
	"testing"
)

func TestAlphabetIsUnbiasedAndUnambiguous(t *testing.T) {
	if 256%len(alphabet) != 0 {
		t.Fatalf("alphabet length %d does not divide 256, so byte%%len would bias generated codes", len(alphabet))
	}
	if strings.ContainsAny(alphabet, "01IO") {
		t.Fatal("alphabet contains visually ambiguous characters")
	}
	seen := map[rune]bool{}
	for _, c := range alphabet {
		if seen[c] {
			t.Fatalf("alphabet repeats %q", c)
		}
		seen[c] = true
	}
}

func TestGenerateLengthAndAlphabet(t *testing.T) {
	for _, n := range []int{0, 1, 6, 8, 32, 64} {
		code, err := Generate(n)
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != n {
			t.Fatalf("Generate(%d) returned %d characters", n, len(code))
		}
		for _, c := range code {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("Generate(%d) emitted %q outside the alphabet", n, c)
			}
		}
	}
}

func TestGenerateRejectsNegativeLength(t *testing.T) {
	code, err := Generate(-1)
	if !errors.Is(err, ErrNegativeLength) || code != "" {
		t.Fatalf("Generate(-1) = (%q, %v), want ErrNegativeLength", code, err)
	}
}

func TestGenerateDoesNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, err := Generate(8)
		if err != nil {
			t.Fatal(err)
		}
		if seen[code] {
			t.Fatalf("duplicate 8-character code %q within 500 draws", code)
		}
		seen[code] = true
	}
}

// With 64000 draws over 32 symbols each expects 2000 hits (sigma about 43);
// the bounds are ten sigma wide so the check cannot flake yet catches a
// skewed mapping.
func TestGenerateSymbolsAreRoughlyUniform(t *testing.T) {
	counts := map[byte]int{}
	for i := 0; i < 2000; i++ {
		code, err := Generate(32)
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < len(code); j++ {
			counts[code[j]]++
		}
	}
	if len(counts) != len(alphabet) {
		t.Fatalf("only %d of %d symbols appeared", len(counts), len(alphabet))
	}
	for symbol, n := range counts {
		if n < 1570 || n > 2430 {
			t.Fatalf("symbol %q appeared %d times, expected about 2000", symbol, n)
		}
	}
}
