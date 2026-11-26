package securestore

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

// TOTP implements RFC 6238's interoperable SHA-1 / 30-second / 6-digit profile.
// The caller must persist and atomically reject already-used counters.
func TOTP(secret string, counter int64, digits int) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(key) < 20 {
		return "", fmt.Errorf("invalid TOTP secret")
	}
	if counter < 0 || (digits != 6 && digits != 8) {
		return "", fmt.Errorf("invalid TOTP parameters")
	}
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], uint64(counter))
	h := hmac.New(sha1.New, key)
	h.Write(data[:])
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 15
	n := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1000000)
	if digits == 8 {
		mod = 100000000
	}
	return fmt.Sprintf("%0*d", digits, n%mod), nil
}
func VerifyTOTP(secret, code string, now time.Time, last int64) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	counter := now.Unix() / 30
	for _, c := range []int64{counter, counter - 1, counter + 1} {
		if c <= last {
			continue
		}
		expected, err := TOTP(secret, c, 6)
		if err == nil && subtle.ConstantTimeCompare([]byte(code), []byte(expected)) == 1 {
			return c, true
		}
	}
	return 0, false
}
