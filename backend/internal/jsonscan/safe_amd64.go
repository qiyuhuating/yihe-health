//go:build amd64 && !purego

package jsonscan

var hasAVX2 = cpuHasAVX2()

func cpuHasAVX2() bool

// Safe reports whether s needs neither JSON escaping nor UTF-8 normalization.
// Short strings use the scalar loop; vector scans select AVX2 when available.
func Safe(s string) bool {
	if len(s) < 16 {
		return safeScalar(s)
	}
	return safeVector(s)
}

//go:noescape
func safeVector(s string) bool

//go:noescape
func safeAVX2(s string) bool
