package deviceprotocol

import "testing"

func TestValidRequestID(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"sos-123_abc:1.2", true},
		{"", false},
		{"a/b", false},
		{"中文", false},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", true},
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
	} {
		if got := ValidRequestID(tc.id); got != tc.want {
			t.Errorf("ValidRequestID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
