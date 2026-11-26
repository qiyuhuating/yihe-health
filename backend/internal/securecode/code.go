package securecode

import (
	"crypto/rand"
	"errors"
)

// The alphabet length must divide 256: Generate maps each random byte with a
// modulo, which is only unbiased in that case. code_test.go enforces it.
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// ErrNegativeLength is returned instead of panicking when n is negative.
var ErrNegativeLength = errors.New("securecode: negative length")

// Generate produces a random identifier without visually ambiguous characters.
func Generate(n int) (string, error) {
	if n < 0 {
		return "", ErrNegativeLength
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i, v := range b {
		b[i] = alphabet[int(v)%len(alphabet)]
	}
	return string(b), nil
}
