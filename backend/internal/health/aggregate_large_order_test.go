package health

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestLargeSleepPackedOrder(t *testing.T) {
	from := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	to := from.Add(48 * time.Hour)
	samples := make([]Sample, 8192)
	for i := range samples {
		at := from.Add(time.Duration(i/3)*time.Second + time.Duration(i%3)*time.Nanosecond)
		samples[i] = Sample{Metric: "sleep_stage", Source: "watch", SourceDevice: "A", Semantics: "interval", Unit: "min", Value: Number(0), Start: at, End: at.Add(time.Duration(1+i%7) * time.Second), Stage: []string{"core", "deep", "awake", "in_bed"}[i%4], Deleted: i%17 == 0}
	}
	want, err := Aggregate(samples, from, to)
	if err != nil {
		t.Fatal(err)
	}
	rand.New(rand.NewSource(17)).Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
	before := slices.Clone(samples)
	got, err := Aggregate(samples, from, to)
	if err != nil || !reflect.DeepEqual(want, got) || !reflect.DeepEqual(before, samples) {
		t.Fatalf("ordered/packed mismatch: got=%+v want=%+v err=%v", got, want, err)
	}
}
