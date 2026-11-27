package rules

import (
	"sync"
	"testing"
	"time"
)

func TestLocationCacheConcurrentBoundedSnapshots(t *testing.T) {
	var c locationCache
	names := []string{"UTC", "Africa/Cairo", "Africa/Nairobi", "Africa/Lagos", "Africa/Johannesburg", "Africa/Accra", "Africa/Algiers", "Africa/Casablanca", "Africa/Dakar", "Africa/Khartoum", "Africa/Tunis", "America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles", "America/Phoenix", "America/Toronto", "America/Vancouver", "America/Mexico_City", "America/Lima", "America/Bogota", "America/Santiago", "America/Sao_Paulo", "America/Caracas", "America/Havana", "America/Guatemala", "America/Anchorage", "America/Halifax", "America/Montevideo", "America/Argentina/Buenos_Aires", "Asia/Shanghai", "Asia/Tokyo", "Asia/Seoul", "Asia/Kolkata", "Asia/Dhaka", "Asia/Kathmandu", "Asia/Bangkok", "Asia/Jakarta", "Asia/Singapore", "Asia/Manila", "Asia/Hong_Kong", "Asia/Taipei", "Asia/Dubai", "Asia/Riyadh", "Asia/Karachi", "Asia/Tehran", "Asia/Jerusalem", "Asia/Baghdad", "Australia/Sydney", "Australia/Perth", "Australia/Brisbane", "Australia/Adelaide", "Australia/Darwin", "Australia/Hobart", "Europe/London", "Europe/Paris", "Europe/Berlin", "Europe/Rome", "Europe/Madrid", "Europe/Lisbon", "Europe/Moscow", "Europe/Athens", "Europe/Helsinki", "Europe/Warsaw", "Europe/Prague", "Europe/Oslo", "Europe/Stockholm", "Europe/Copenhagen", "Europe/Zurich", "Pacific/Auckland", "Pacific/Honolulu", "Pacific/Fiji"}
	at := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	expected := make([]time.Time, len(names))
	for i, name := range names {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		expected[i] = at.In(loc)
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Go(func() {
			for n := range len(names) * 2 {
				i := (n + worker*13) % len(names)
				loc, err := c.get(names[i])
				if err != nil {
					t.Error(err)
					return
				}
				got := at.In(loc)
				gn, goff := got.Zone()
				wn, woff := expected[i].Zone()
				if gn != wn || goff != woff {
					t.Errorf("wrong timezone %s", names[i])
					return
				}
				if snapshot := c.values.Load(); snapshot == nil || len(*snapshot) > 64 {
					t.Error("cache cap")
					return
				}
			}
		})
	}
	wg.Wait()
	if _, err := c.get("Invalid/Timezone"); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	if _, ok := (*c.values.Load())["Invalid/Timezone"]; ok {
		t.Fatal("invalid timezone retained")
	}
}

func BenchmarkNightParallel(b *testing.B) {
	p, now := Default(), benchmarkInput().Now
	_ = p.Night(now)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !p.Night(now) {
				b.Error("fixture outside night")
				return
			}
		}
	})
}
