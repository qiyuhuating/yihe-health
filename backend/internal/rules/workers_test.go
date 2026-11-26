package rules

import "testing"

func TestDeviceWorkers(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"", 8}, {"1", 1}, {"4", 4}, {"8", 8},
		{" 2 ", 2}, {"0", 8}, {"9", 8}, {"-1", 8}, {"bad", 8},
	} {
		if got := deviceWorkers(tc.value); got != tc.want {
			t.Errorf("deviceWorkers(%q) = %d, want %d", tc.value, got, tc.want)
		}
	}
}
