package jsonscan

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestSafeEquivalent(t *testing.T) {
	for _, n := range []int{0, 1, 15, 16, 17, 31, 32, 33, 63, 64, 65, 128} {
		for offset := 0; offset < 16; offset++ {
			b := []byte(strings.Repeat("a", n+offset))
			s := string(b)[offset:]
			if Safe(s) != safeScalar(s) {
				t.Fatalf("safe length %d offset %d", n, offset)
			}
			for i := offset; i < len(b); i++ {
				for v := 0; v < 256; v++ {
					b[i] = byte(v)
					s = string(b)[offset:]
					if Safe(s) != safeScalar(s) {
						t.Fatalf("length=%d position=%d byte=%x", n, i-offset, v)
					}
				}
				b[i] = 'a'
			}
		}
	}
	rng := rand.New(rand.NewSource(71))
	for i := 0; i < 2000; i++ {
		b := make([]byte, rng.Intn(2048))
		rng.Read(b)
		s := string(b)
		if Safe(s) != safeScalar(s) {
			t.Fatalf("random %d", i)
		}
	}
}

var result bool

func BenchmarkSafe(b *testing.B) {
	for _, n := range []int{8, 16, 32, 64, 256, 1024} {
		for _, kind := range []string{"ascii", "unicode-first", "escape-last"} {
			s := strings.Repeat("a", n)
			if kind == "unicode-first" {
				s = "中" + s[3:]
			}
			if kind == "escape-last" {
				s = s[:n-1] + "\\"
			}
			b.Run(fmt.Sprintf("%s/%d/Go", kind, n), func(b *testing.B) {
				b.SetBytes(int64(n))
				for i := 0; i < b.N; i++ {
					result = safeScalar(s)
				}
			})
			b.Run(fmt.Sprintf("%s/%d/Selected", kind, n), func(b *testing.B) {
				b.SetBytes(int64(n))
				for i := 0; i < b.N; i++ {
					result = Safe(s)
				}
			})
		}
	}
}
