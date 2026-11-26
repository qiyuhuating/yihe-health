package devicecredential

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"yihe-care/internal/securecode"
)

const hashPrefix = "sha256$"
const hashDomain = "yihe-device-secret-v1"

var ErrSecretLength = errors.New("invalid device credential length")

func Hash(clear string) (string, error) {
	if len(clear) < 24 || len(clear) > 64 {
		return "", ErrSecretLength
	}
	h := sha256.New()
	h.Write([]byte(hashDomain))
	h.Write([]byte{0})
	h.Write([]byte(clear))
	return hashPrefix + base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}

func Matches(supplied, stored string) bool {
	if len(supplied) < 24 || len(supplied) > 64 ||
		!strings.HasPrefix(stored, hashPrefix) || len(stored) != len(hashPrefix)+43 {
		return false
	}
	if _, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(stored, hashPrefix)); err != nil {
		return false
	}
	expected, err := Hash(supplied)
	return err == nil && subtle.ConstantTimeCompare([]byte(expected), []byte(stored)) == 1
}

// Failure identifies why a device credential cannot be used.
type Failure uint8

const (
	Valid Failure = iota
	InvalidSecret
	Disabled
)

// Check applies the same credential and enabled-state policy to every device entry point.
func Check(supplied, stored string, status *int) Failure {
	if !Matches(supplied, stored) {
		return InvalidSecret
	}
	if status != nil && *status == 0 {
		return Disabled
	}
	return Valid
}

func Generate() (clear, hashed string, err error) {
	clear, err = securecode.Generate(32)
	if err != nil {
		return "", "", err
	}
	hashed, err = Hash(clear)
	return clear, hashed, err
}
