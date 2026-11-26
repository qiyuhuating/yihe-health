package rules

import (
	"fmt"
	"testing"
	"time"

	"yihe-care/internal/health"
)

// Reference preserves the previous slice-and-Aggregate implementation so the
// streaming calculation is checked against behavior, not its own internals.
func referenceActivity(p Policy, samples []health.Sample, from, to time.Time) (float64, bool) {
	filtered := []health.Sample{}
	reset := false
	var previous *float64
	var semantics string
	for _, sample := range samples {
		if sample.Value == nil || !p.Night(sample.Start) || sample.TimeBasis == "legacy-unzoned" {
			continue
		}
		if semantics != "" && semantics != sample.Semantics {
			reset = true
		}
		semantics = sample.Semantics
		if semantics == "daily_total" && previous != nil && *sample.Value < *previous {
			reset = true
		}
		value := *sample.Value
		previous = &value
		filtered = append(filtered, sample)
	}
	if reset || len(filtered) == 0 || len(filtered) > 1000 {
		return 0, false
	}
	if semantics == "daily_total" {
		if len(filtered) < 2 {
			return 0, false
		}
		return *filtered[len(filtered)-1].Value - *filtered[0].Value, true
	}
	stats, err := health.Aggregate(filtered, from, to)
	if err != nil || stats.Total == nil {
		return 0, false
	}
	return *stats.Total, true
}

func activitySamples(n int, semantics string) (Policy, []health.Sample, time.Time, time.Time) {
	p, now := Default(), benchmarkInput().Now
	from := now.Add(-15 * time.Minute)
	samples := make([]health.Sample, n)
	for i := range samples {
		at := from.Add(time.Duration(i+1) * 500 * time.Millisecond)
		samples[i] = health.Sample{Source: "watch", SourceDevice: "one", Metric: "steps", Semantics: semantics, Value: health.Number(float64(i + 100)), Start: at, End: at.Add(100 * time.Millisecond)}
		if semantics == "daily_total" {
			samples[i].End = at
		}
	}
	return p, samples, from, now
}

func TestActivityWindowMatchesHistoricalSemantics(t *testing.T) {
	for _, semantics := range []string{"daily_total", "interval"} {
		for _, scenario := range []string{"valid", "reset", "mixed", "overlap", "start_boundary", "end_boundary", "missing", "legacy", "daytime", "empty", "single", "overflow", "deleted", "all_deleted", "source_change", "device_change", "metric_change"} {
			t.Run(semantics+"/"+scenario, func(t *testing.T) {
				p, samples, from, to := activitySamples(3, semantics)
				switch scenario {
				case "reset":
					samples[1].Value = health.Number(1)
				case "mixed":
					samples[1].Semantics = "instant"
				case "overlap":
					samples[1].Start = samples[0].Start.Add(time.Nanosecond)
				case "start_boundary":
					samples[0].Start = from.Add(-time.Second)
				case "end_boundary":
					samples[2].End = to.Add(time.Second)
				case "missing":
					samples[1].Value = nil
				case "legacy":
					samples[1].TimeBasis = "legacy-unzoned"
				case "daytime":
					samples[0].Start = from.Add(-12 * time.Hour)
				case "empty":
					samples = nil
				case "single":
					samples = samples[:1]
				case "overflow":
					p, samples, from, to = activitySamples(1001, semantics)
				case "deleted":
					samples[0].Deleted = true
				case "all_deleted":
					for i := range samples {
						samples[i].Deleted = true
					}
				case "source_change":
					samples[1].Source = "other"
				case "device_change":
					samples[1].SourceDevice = "other"
				case "metric_change":
					samples[1].Metric = "other"
				}
				want, wantOK := referenceActivity(p, samples, from, to)
				var window activityWindow
				for _, sample := range samples {
					window.add(p, sample, from, to)
				}
				got, ok := window.value()
				if ok != wantOK || ok && got != want {
					t.Fatalf("got (%v,%v), want (%v,%v)", got, ok, want, wantOK)
				}
			})
		}
	}
}

func TestActivityNightMatchesPolicyAcrossChanges(t *testing.T) {
	previousPolicy := Default()
	for _, zone := range []string{"UTC", "America/New_York", "Australia/Lord_Howe", "Asia/Shanghai", "Invalid/Timezone", "", "Local", "UTC"} {
		for _, bounds := range [][2]int{{22 * 3600, 6 * 3600}, {3600, 5 * 3600}, {0, 86399}, {3600, 3600}} {
			p := Default()
			p.Timezone, p.NightBegin, p.NightEnd = zone, bounds[0], bounds[1]
			for _, date := range []string{"2026-03-08T00:00:00Z", "2026-11-01T00:00:00Z", "2026-10-04T00:00:00Z"} {
				from, _ := time.Parse(time.RFC3339, date)
				for i := 0; i < 48*60; i += 17 {
					at := from.Add(time.Duration(i) * time.Minute)
					sample := health.Sample{Semantics: "interval", Metric: "activity", Value: health.Number(1), Start: at, End: at.Add(time.Second)}
					var window activityWindow
					window.add(previousPolicy, sample, at, sample.End)
					window.add(p, sample, at, sample.End)
					want := 0.0
					if previousPolicy.Night(at) {
						want++
					}
					if p.Night(at) {
						want++
					}
					if got, ok := window.value(); got != want || ok != (want > 0) {
						t.Fatalf("zone=%s bounds=%v at=%s got=(%v,%v) want=(%v,%v)", zone, bounds, at, got, ok, want, want > 0)
					}
				}
			}
			previousPolicy = p
		}
	}
}

// This oracle uses formatted dates and resolves both observation dates afresh,
// retaining the original counter-reset semantics independently of the cache.
func referenceDailyActivity(p Policy, samples []health.Sample) (float64, bool) {
	var previous *health.Sample
	var counterZone string
	var counterOffset, count int
	total := 0.0
	for i := range samples {
		s := &samples[i]
		if s.Value == nil || !p.Night(s.Start) || s.TimeBasis == "legacy-unzoned" {
			continue
		}
		zone, loc := s.Metadata["counterTimezone"], s.Start.Location()
		_, offset := s.Start.Zone()
		if zone != "" {
			var err error
			loc, err = ruleLocation(zone)
			if err != nil || zone == "Local" {
				return 0, false
			}
		}
		if previous == nil {
			counterZone, counterOffset = zone, offset
		} else if zone != counterZone || zone == "" && offset != counterOffset || s.Start.Before(previous.Start) {
			return 0, false
		} else if s.Start.In(loc).Format("2006-01-02") != previous.Start.In(loc).Format("2006-01-02") {
			total += *s.Value
		} else if *s.Value < *previous.Value || s.Start.Equal(previous.Start) && *s.Value != *previous.Value {
			return 0, false
		} else {
			total += *s.Value - *previous.Value
		}
		if !(*s.Value >= 0 && *s.Value <= 9007199254740991) {
			return 0, false
		}
		previous = s
		count++
	}
	if count == 0 || count > 1000 {
		return 0, false
	}
	return total, count >= 2 && total <= 9007199254740991
}

func TestActivityCounterCalendarEquivalent(t *testing.T) {
	for _, counterZone := range []string{"", "UTC", "Asia/Shanghai", "America/New_York", "Australia/Lord_Howe", "Invalid/Timezone", "Local"} {
		for _, date := range []string{"2026-03-07T22:00:00Z", "2026-10-03T22:00:00Z", "2026-10-31T22:00:00Z"} {
			for _, scenario := range []string{"valid", "reset", "rollback", "conflict", "reorder", "mixed_zone", "deleted", "missing", "legacy"} {
				p := Default()
				p.Timezone, p.NightBegin, p.NightEnd = "UTC", 0, 86399
				from, _ := time.Parse(time.RFC3339, date)
				loc, err := time.LoadLocation("America/New_York")
				if err != nil {
					t.Fatal(err)
				}
				samples := make([]health.Sample, 80)
				for i := range samples {
					at := from.Add(time.Duration(i) * 37 * time.Minute).In(loc)
					if i%2 == 0 {
						_, offset := at.Zone()
						at = at.In(time.FixedZone("", offset))
					}
					samples[i] = health.Sample{Semantics: "daily_total", Value: health.Number(float64(100 + i)), Start: at, Metadata: map[string]string{"counterTimezone": counterZone}}
				}
				switch scenario {
				case "reset":
					samples = samples[:3]
					samples[0].Start = from
					samples[1].Start = from.Add(3 * time.Hour)
					samples[2].Start = from.Add(4 * time.Hour)
					samples[1].Value, samples[2].Value = health.Number(10), health.Number(20)
				case "rollback":
					samples[1].Value = health.Number(1)
				case "conflict":
					samples[1].Start = samples[0].Start
				case "reorder":
					samples[0], samples[1] = samples[1], samples[0]
				case "mixed_zone":
					samples[1].Metadata["counterTimezone"] = "Asia/Tokyo"
				case "deleted":
					samples[0].Deleted = true
				case "missing":
					samples[0].Value = nil
				case "legacy":
					samples[0].TimeBasis = "legacy-unzoned"
				}
				want, wantOK := referenceDailyActivity(p, samples)
				var window activityWindow
				for _, sample := range samples {
					window.add(p, sample, from, from.Add(48*time.Hour))
				}
				if got, ok := window.value(); got != want || ok != wantOK {
					t.Fatalf("zone=%s date=%s scenario=%s got=(%v,%v) want=(%v,%v)", counterZone, date, scenario, got, ok, want, wantOK)
				}
			}
		}
	}
}

func TestActivityCounterLocationChangeReinterpretsPreviousDate(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	p := Default()
	p.Timezone, p.NightBegin, p.NightEnd = "UTC", 0, 86399
	from := time.Date(2026, time.March, 8, 4, 30, 0, 0, time.UTC)
	// Both samples encode UTC-4, but New York still used UTC-5 at the first
	// instant. Its counter date must be reevaluated in the second location.
	samples := []health.Sample{
		{Semantics: "daily_total", Value: health.Number(100), Start: from.In(time.FixedZone("", -4*3600))},
		{Semantics: "daily_total", Value: health.Number(250), Start: from.Add(3 * time.Hour).In(loc)},
	}
	var window activityWindow
	for _, sample := range samples {
		window.add(p, sample, from, from.Add(4*time.Hour))
	}
	if got, ok := window.value(); !ok || got != 250 {
		t.Fatalf("got=(%v,%v), want=(250,true)", got, ok)
	}
}

var benchmarkActivityValue float64
var benchmarkActivityValid bool

// This benchmark compares only aggregation of an already decoded ordered SQL
// window, excluding database reads and JSON decoding.
func BenchmarkActivityWindow(b *testing.B) {
	for _, n := range []int{100, 1000} {
		for _, semantics := range []string{"daily_total", "interval"} {
			p, samples, from, to := activitySamples(n, semantics)
			b.Run(fmt.Sprintf("%s/%d/reference", semantics, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					benchmarkActivityValue, benchmarkActivityValid = referenceActivity(p, samples, from, to)
				}
			})
			b.Run(fmt.Sprintf("%s/%d/stream", semantics, n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var window activityWindow
					for _, sample := range samples {
						window.add(p, sample, from, to)
					}
					benchmarkActivityValue, benchmarkActivityValid = window.value()
				}
			})
		}
	}
}
