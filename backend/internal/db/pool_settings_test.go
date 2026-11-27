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

func TestMySQLSOSConnectionLimit(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		fail bool
	}{{"", 0, false}, {"2", 2, false}, {"8", 8, false}, {"9", 0, true}, {"-1", 0, true}, {"bad", 0, true}} {
		got, err := mysqlOptionalConnectionLimit(tc.raw, "MYSQL_SOS_MAX_CONNS", 8)
		if got != tc.want || (err != nil) != tc.fail {
			t.Fatalf("%q: %d %v", tc.raw, got, err)
		}
	}
}

func TestMySQLBusinessConnectionLimit(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int
		fail bool
	}{{"", 20, false}, {"4", 4, false}, {"64", 64, false}, {"0", 0, true}, {"65", 0, true}, {"bad", 0, true}} {
		got, err := mysqlBusinessConnectionLimit(tc.raw)
		if got != tc.want || (err != nil) != tc.fail {
			t.Fatalf("%q: %d %v", tc.raw, got, err)
		}
	}
}

func TestMySQLIdleConnectionBudget(t *testing.T) {
	for _, tc := range []struct {
		raw        string
		open, want int
		fail       bool
	}{{"", 4, 4, false}, {"", 64, 20, false}, {"32", 64, 32, false}, {"5", 4, 0, true}} {
		got, err := mysqlIdleConnectionLimit(tc.raw, tc.open)
		if got != tc.want || (err != nil) != tc.fail {
			t.Fatalf("%q with budget %d: %d %v", tc.raw, tc.open, got, err)
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
		got, err := mysqlIdleConnectionLimit(tc.input, 20)
		if (err != nil) != tc.error || (!tc.error && got != tc.want) {
			t.Errorf("mysqlIdleConnectionLimit(%q)=(%d,%v), want (%d,error=%t)", tc.input, got, err, tc.want, tc.error)
		}
	}
}
