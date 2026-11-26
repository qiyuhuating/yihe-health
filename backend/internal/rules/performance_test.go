package rules

import (
	"testing"
	"time"
)

var benchmarkNight bool

func benchmarkInput() Input {
	now := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC)
	return Input{Now: now, LastReport: now.Add(-10 * time.Minute),
		Heart:     &Value{100, "heart:1", "watch", "RULE001", now},
		Blood:     &Value{155, "blood:1", "watch", "RULE001", now},
		Battery:   &Value{10, "battery:1", "watch", "RULE001", now},
		Activity:  &Value{250, "steps:1", "watch", "RULE001", now},
		Baselines: map[string]Baseline{"heart_rate": {20, 70, 2}, "blood_pressure": {20, 115, 5}},
	}
}

func BenchmarkNight(b *testing.B) {
	p, now := Default(), benchmarkInput().Now
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkNight = p.Night(now)
	}
}

func TestNightBoundariesAndDST(t *testing.T) {
	p := Default()
	for _, tc := range []struct {
		zone, at   string
		begin, end int
		want       bool
	}{
		{"Asia/Shanghai", "2026-10-20T13:59:59Z", 22 * 3600, 6 * 3600, false},
		{"Asia/Shanghai", "2026-10-20T14:00:00Z", 22 * 3600, 6 * 3600, true},
		{"Asia/Shanghai", "2026-10-20T22:00:00Z", 22 * 3600, 6 * 3600, false},
		{"UTC", "2026-10-20T10:00:00Z", 10 * 3600, 12 * 3600, true},
		{"UTC", "2026-10-20T12:00:00Z", 10 * 3600, 12 * 3600, false},
		{"America/New_York", "2026-11-01T05:30:00Z", 3600, 2 * 3600, true},
		{"America/New_York", "2026-11-01T06:30:00Z", 3600, 2 * 3600, true},
		{"Invalid/Timezone", "2026-10-20T14:00:00Z", 22 * 3600, 6 * 3600, false},
	} {
		p.Timezone, p.NightBegin, p.NightEnd = tc.zone, tc.begin, tc.end
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Night(at); got != tc.want {
			t.Errorf("zone=%s at=%s: got %v want %v", tc.zone, tc.at, got, tc.want)
		}
	}
}

func TestEvaluateThresholdAndFreshnessBoundaries(t *testing.T) {
	p := Default()
	in := benchmarkInput()
	in.Blood, in.Battery, in.Activity = nil, nil, nil
	in.LastReport = time.Time{}
	in.Baselines = nil
	for _, tc := range []struct {
		value  float64
		age    time.Duration
		count  int
		breach bool
	}{
		{p.HRLow, 0, 1, false}, {p.HRHigh, 0, 1, false},
		{p.HRLow - 1, 0, 1, true}, {p.HRHigh + 1, 0, 1, true},
		{100, 5 * time.Minute, 1, false}, {100, 5*time.Minute + 1, 0, false},
		{100, -time.Minute, 1, false}, {100, -time.Minute - 1, 0, false},
	} {
		in.Heart.Value, in.Heart.At = tc.value, in.Now.Add(-tc.age)
		got := EvaluateCompact(p, in)
		if len(got) != tc.count || len(got) > 0 && (got[0].Breach != tc.breach || got[0].Required != p.Consecutive || got[0].Severity != "critical") {
			t.Fatalf("value=%v age=%s: %+v", tc.value, tc.age, got)
		}
	}
}
