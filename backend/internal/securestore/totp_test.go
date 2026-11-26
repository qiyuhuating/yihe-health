package securestore

import (
	"encoding/base32"
	"testing"
	"time"
)

func TestRFC6238VectorsAndReplay(t *testing.T) {
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		second int64
		code   string
	}{{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"}, {1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"}} {
		got, err := TOTP(s, v.second/30, 8)
		if err != nil || got != v.code {
			t.Fatal(v, got, err)
		}
	}
	now := time.Unix(1234567890, 0)
	code, _ := TOTP(s, now.Unix()/30, 6)
	c, ok := VerifyTOTP(s, code, now, -1)
	if !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok = VerifyTOTP(s, code, now, c); ok {
		t.Fatal("replay accepted")
	}
}
