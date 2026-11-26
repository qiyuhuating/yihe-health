package db

import "testing"

func TestMySQLStatisticsConnectionLimit(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		fail bool
	}{{"", 0, false}, {"12", 12, false}, {"64", 64, false}, {"65", 0, true}, {"-1", 0, true}, {"bad", 0, true}} {
		got, err := mysqlStatisticsConnectionLimit(tc.raw)
		if got != tc.want || (err != nil) != tc.fail {
			t.Fatalf("%q: %d %v", tc.raw, got, err)
		}
	}
}

func TestMySQLIdleConnectionLimit(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  int
		error bool
	}{
		{"", 20, false},
		{"  ", 20, false},
		{"0", 0, false},
		{"5", 5, false},
		{"10", 10, false},
		{"20", 20, false},
		{"-1", 0, true},
		{"21", 0, true},
		{"10000", 0, true},
		{"not-an-integer", 0, true},
	} {
		got, err := mysqlIdleConnectionLimit(tc.input)
		if (err != nil) != tc.error || (!tc.error && got != tc.want) {
			t.Errorf("mysqlIdleConnectionLimit(%q)=(%d,%v), want (%d,error=%t)", tc.input, got, err, tc.want, tc.error)
		}
	}
}
