package careworkflow

import "testing"

func TestNextState(t *testing.T) {
	cases := []struct {
		before string
		action string
		want   string
	}{
		{"received", "acknowledge", "acknowledged"},
		{"notifying", "escalate", "escalated"},
		{"notified", "acknowledge", "acknowledged"},
		{"escalated", "acknowledge", "acknowledged"},
		{"acknowledged", "start", "in_progress"},
		{"in_progress", "resolve", "resolved"},
		{"resolved", "verify", "verified"},
		{"review_overdue", "reopen", "in_progress"},
		{"verified", "reopen", ""},
		{"received", "verify", ""},
		{"in_progress", "verify", ""},
	}
	for _, tc := range cases {
		if got := NextState(tc.before, tc.action); got != tc.want {
			t.Errorf("NextState(%q, %q) = %q, want %q", tc.before, tc.action, got, tc.want)
		}
	}
}
