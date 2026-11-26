//go:build amd64 && !purego

package jsonscan

import (
	"strings"
	"testing"
)

func TestVectorDispatchFallback(t *testing.T) {
	original := hasAVX2
	defer func() { hasAVX2 = original }()

	for _, enabled := range []bool{false, original} {
		hasAVX2 = enabled
		for _, n := range []int{63, 64, 65, 95, 96, 97, 127, 128, 129, 255, 256, 257} {
			p := []byte(strings.Repeat("a", n))
			if !Safe(string(p)) {
				t.Fatalf("avx2=%t length=%d rejected ASCII", enabled, n)
			}
			for _, i := range []int{0, 31, 32, n - 1} {
				for b := 0; b < 256; b++ {
					p[i] = byte(b)
					s := string(p)
					if Safe(s) != safeScalar(s) {
						t.Fatalf("avx2=%t length=%d offset=%d byte=%x", enabled, n, i, b)
					}
				}
				p[i] = 'a'
			}
		}
	}
}
