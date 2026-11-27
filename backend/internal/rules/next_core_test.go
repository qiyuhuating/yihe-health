//go:build yihe_next_gateway

package rules

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"
	"yihe-care/internal/health"
)

func nextSamples(n int) []health.Sample {
	samples := make([]health.Sample, n)
	for i := range samples {
		at := time.Unix(0, int64(i+1))
		samples[i] = health.Sample{Source: "watch", SourceDevice: "fixture", Metric: "heart_rate", Unit: "bpm", Semantics: "instant", Start: at, End: at, Value: health.Number(float64(((i + 1) * 31) % 251))}
	}
	return samples
}

func TestNextCoreDifferential(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Fatal("YIHE_NEXT_GATEWAY_BINARY required")
	}
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	var expected []any
	add := func(kind string, body, want any) {
		t.Helper()
		if err := enc.Encode(map[string]any{"kind": kind, "body": body}); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		var normalized any
		if err = json.Unmarshal(raw, &normalized); err != nil {
			t.Fatal(err)
		}
		expected = append(expected, normalized)
	}
	const count = 2048
	samples := nextSamples(count)
	values := make([]int, count)
	for i, s := range samples {
		values[i] = int(*s.Value)
	}
	for _, width := range []int{1, 8, 64, 256, 1024} {
		want := make([][]any, 0, count)
		for i := range samples {
			start := max(0, i-width+1)
			stats, err := health.Aggregate(samples[start:i+1], samples[start].Start, samples[i].Start.Add(time.Nanosecond))
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, []any{stats.Count, *stats.Min, *stats.Max, *stats.Mean})
		}
		add("rolling", map[string]any{"width": width, "values": values}, want)
	}
	now := time.Date(2026, 10, 20, 15, 30, 0, 0, time.UTC)
	vals := []float64{0, 19, 20, 54, 55, 56, 89, 90, 91, 109, 110, 111, 139, 140, 141, 199, 200, 201, 250}
	ages := []time.Duration{-61 * time.Second, -60 * time.Second, 0, 300 * time.Second, 300*time.Second + time.Nanosecond}
	for i := range 1200 {
		p := Default()
		p.Enabled = i%17 != 0
		at := now.Add(time.Duration(i%24) * time.Hour)
		v := func(offset int) *Value {
			if (i+offset)%7 == 0 {
				return nil
			}
			return &Value{Value: vals[(i*3+offset)%len(vals)], Key: "fixture", Source: "watch", Device: "fixture", At: at.Add(-ages[(i+offset)%len(ages)])}
		}
		input := Input{Now: at, Heart: v(1), Blood: v(2), Battery: v(3), Activity: v(4), Baselines: map[string]Baseline{"heart_rate": {Count: i % 25, Mean: 80, Deviation: 4}, "blood_pressure": {Count: (i + 11) % 25, Mean: 120, Deviation: 6}}}
		if i%5 != 0 {
			input.LastReport = at.Add(-ages[i%len(ages)])
		}
		obs := func(v *Value) any {
			if v == nil {
				return nil
			}
			return map[string]any{"value": v.Value, "at_ns": v.At.UnixNano()}
		}
		base := func(b Baseline) any {
			return map[string]any{"count": b.Count, "mean": b.Mean, "deviation": b.Deviation}
		}
		var last any
		if !input.LastReport.IsZero() {
			last = input.LastReport.UnixNano()
		}
		var dst [7]CompactCandidate
		candidates := EvaluateCompactInto(dst[:0], p, input)
		want := make([][]any, 0, len(candidates))
		for _, c := range candidates {
			want = append(want, []any{c.Kind, c.Severity, c.Breach, c.Required})
		}
		add("rules", map[string]any{"enabled": p.Enabled, "night": p.Night(at), "activity_night": input.Activity != nil && p.Night(input.Activity.At), "now_ns": at.UnixNano(), "last_report_ns": last, "heart": obs(input.Heart), "blood": obs(input.Blood), "battery": obs(input.Battery), "activity": obs(input.Activity), "heart_baseline": base(input.Baselines["heart_rate"]), "blood_baseline": base(input.Baselines["blood_pressure"])}, want)
	}
	cmd := exec.CommandContext(t.Context(), binary, "--oracle")
	cmd.Stdin = &in
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for i, want := range expected {
		if !scanner.Scan() {
			t.Fatalf("missing oracle result %d: %v", i, scanner.Err())
		}
		var got any
		if err = json.Unmarshal(scanner.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("oracle result %d differs", i)
		}
	}
	if scanner.Scan() || scanner.Err() != nil {
		t.Fatal("unexpected oracle output")
	}
	t.Logf("%d rolling window results and 1200 actual Go rule candidate sets match", count*5)
}

func TestNextCoreTransitions(t *testing.T) {
	binary := os.Getenv("YIHE_NEXT_GATEWAY_BINARY")
	if binary == "" {
		t.Fatal("YIHE_NEXT_GATEWAY_BINARY required")
	}
	db, now := seed(t)
	engine := Engine{DB: db}
	p := Default()
	ctx := t.Context()
	var events []map[string]any
	var want [][]any
	prior := map[string]string{}
	for i, event := range []struct {
		key, source string
		value       float64
		offline     int
	}{
		{"a", "watch", 120, 0}, {"a", "watch", 120, 0}, {"b", "watch", 120, 0}, {"c", "watch", 120, 0},
		{"d", "watch", 120, 0}, {"e", "other", 120, 0}, {"f", "other", 120, 0}, {"g", "other", 120, 0},
		{"h", "other", 80, 0}, {"i", "other", 120, 0}, {"j", "other", 120, 0}, {"k", "other", 120, 0},
		{"", "", 0, 301}, {"", "", 0, 301}, {"", "", 0, 300}, {"", "", 0, 302},
	} {
		var in Input
		in.Now = now
		if event.key != "" {
			in.Heart = &Value{Value: event.value, Key: event.key, Source: event.source, Device: "RULE001", At: now}
		} else {
			in.LastReport = now.Add(-time.Duration(event.offline) * time.Second)
		}
		var scratch [7]CompactCandidate
		candidates := EvaluateCompactInto(scratch[:0], p, in)
		if len(candidates) != 1 {
			t.Fatalf("fixture %d candidate count %d", i, len(candidates))
		}
		c := candidates[0]
		count := func() int {
			t.Helper()
			var n int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM care_incident WHERE device_id=1 AND kind=?", "rule_"+c.Kind).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		before := count()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = engine.persist(ctx, tx, 1, 1, 1, c, now, nil); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var streak int
		var active bool
		if err = db.QueryRowContext(ctx, "SELECT streak,active FROM rule_state WHERE device_id=1 AND epoch=1 AND kind=?", c.Kind).Scan(&streak, &active); err != nil {
			t.Fatal(err)
		}
		action := "updated"
		if prior[c.Kind] == c.Key {
			action = "duplicate"
		} else if count() > before {
			action = "activate"
		}
		prior[c.Kind] = c.Key
		events = append(events, map[string]any{"kind": c.Kind, "key": c.Key, "source": c.evidence.sourceValue(), "breach": c.Breach, "required": c.Required})
		want = append(want, []any{action, streak, active})
	}
	raw, err := json.Marshal(map[string]any{"kind": "transitions", "body": events})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "--oracle")
	cmd.Stdin = bytes.NewReader(append(raw, '\n'))
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var got any
	if err = json.Unmarshal(output, &got); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	if err = json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("Rust transitions %v != persisted Go %v", got, expected)
	}
	t.Logf("%d Rust transitions match actual MySQL Go rule state and incident creation", len(want))
}

// Test-only algorithm-equivalent control; not a production Go replacement.
type nextRollingControl struct {
	timestamps   []int64
	values       []uint8
	head, len    int
	counts       [256]uint16
	occupied     [4]uint64
	sum, squares uint64
}

func (r *nextRollingControl) insert(at int64, value uint8) {
	tail := (r.head + r.len) % len(r.values)
	r.timestamps[tail] = at
	r.values[tail] = value
	r.len++
	i := int(value)
	r.counts[i]++
	r.occupied[i/64] |= 1 << uint(i%64)
	v := uint64(value)
	r.sum += v
	r.squares += v * v
}
func (r *nextRollingControl) expire(from int64) {
	for r.len != 0 && r.timestamps[r.head] < from {
		value := r.values[r.head]
		r.head = (r.head + 1) % len(r.values)
		r.len--
		i := int(value)
		r.counts[i]--
		if r.counts[i] == 0 {
			r.occupied[i/64] &^= 1 << uint(i%64)
		}
		v := uint64(value)
		r.sum -= v
		r.squares -= v * v
	}
}
func (r *nextRollingControl) checksum() float64 {
	low, high := 0, 3
	for r.occupied[low] == 0 {
		low++
	}
	for r.occupied[high] == 0 {
		high--
	}
	return float64(r.len) + float64(low*64+bits.TrailingZeros64(r.occupied[low])) + float64(high*64+63-bits.LeadingZeros64(r.occupied[high])) + float64(r.sum)/float64(r.len)
}
func BenchmarkNextGoRollingControl(b *testing.B) {
	const events = 4096
	for _, width := range []int{64, 256, 1024, 4096} {
		b.Run(fmt.Sprint(width), func(b *testing.B) {
			samples := nextSamples(width + events)
			var checksum float64
			r := nextRollingControl{timestamps: make([]int64, width), values: make([]uint8, width)}
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				r.head, r.len, r.sum, r.squares = 0, 0, 0, 0
				r.counts = [256]uint16{}
				r.occupied = [4]uint64{}
				for i := range width {
					r.insert(int64(i+1), uint8(*samples[i].Value))
				}
				b.StartTimer()
				for i := width; i < width+events; i++ {
					r.expire(int64(i - width + 2))
					r.insert(int64(i+1), uint8(*samples[i].Value))
					checksum += r.checksum()
				}
			}
			b.StopTimer()
			nextBenchmarkChecksum = checksum
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*events), "ns/event")
			// Check all outputs against actual production aggregate outside timing.
			r.head, r.len, r.sum, r.squares = 0, 0, 0, 0
			r.counts = [256]uint16{}
			r.occupied = [4]uint64{}
			for i := range width {
				r.insert(int64(i+1), uint8(*samples[i].Value))
			}
			for i := width; i < width+events; i++ {
				r.expire(int64(i - width + 2))
				r.insert(int64(i+1), uint8(*samples[i].Value))
				s, err := health.Aggregate(samples[i-width+1:i+1], samples[i-width+1].Start, samples[i].Start.Add(time.Nanosecond))
				if err != nil || r.checksum() != float64(s.Count)+*s.Min+*s.Max+*s.Mean {
					b.Fatalf("control differs at %d", i)
				}
			}
		})
	}
}

var nextBenchmarkChecksum float64

func BenchmarkNextGoWindow(b *testing.B) {
	const events = 4096
	for _, width := range []int{64, 256, 1024, 4096} {
		b.Run(fmt.Sprint(width), func(b *testing.B) {
			samples := nextSamples(width + events)
			var checksum float64
			b.ResetTimer()
			for range b.N {
				for i := width; i < width+events; i++ {
					s, err := health.Aggregate(samples[i-width+1:i+1], samples[i-width+1].Start, samples[i].Start.Add(time.Nanosecond))
					if err != nil {
						b.Fatal(err)
					}
					checksum += float64(s.Count) + *s.Min + *s.Max + *s.Mean
				}
			}
			b.StopTimer()
			nextBenchmarkChecksum = checksum
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*events), "ns/event")
		})
	}
}
