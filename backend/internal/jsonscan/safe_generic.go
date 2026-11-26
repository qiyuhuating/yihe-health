//go:build !amd64 || purego

package jsonscan

// Safe reports whether s needs neither JSON escaping nor UTF-8 normalization.
func Safe(s string) bool { return safeScalar(s) }
