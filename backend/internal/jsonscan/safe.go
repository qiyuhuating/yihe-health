// Package jsonscan recognizes strings that encoding/json can emit without escaping.
package jsonscan

// The table is immutable after initialization and mirrors encoding/json's
// escaping rules for control, non-ASCII, quote, backslash and HTML-sensitive bytes.
var needsEscape = func() [256]uint8 {
	var table [256]uint8
	for i := 0; i < 0x20; i++ {
		table[i] = 1
	}
	for i := 0x80; i < 256; i++ {
		table[i] = 1
	}
	for _, c := range []byte{'"', '\\', '<', '>', '&'} {
		table[c] = 1
	}
	return table
}()

func safeScalar(s string) bool {
	for len(s) >= 4 {
		if needsEscape[s[0]]|needsEscape[s[1]]|needsEscape[s[2]]|needsEscape[s[3]] != 0 {
			return false
		}
		s = s[4:]
	}
	for i := 0; i < len(s); i++ {
		if needsEscape[s[i]] != 0 {
			return false
		}
	}
	return true
}
