package health

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestBatchValueArenaOwnership(t *testing.T) {
	_, base := sampleBenchmarkInput()
	samples := make([]Sample, 200)
	for i := range samples {
		s := base
		s.RecordID = strconv.Itoa(i)
		s.Value = Number(float64(i))
		switch i % 5 {
		case 1:
			s.Metric, s.Unit = "body_mass", "lb"
		case 2:
			s.Metric, s.Unit = "oxygen_saturation", "fraction"
			s.Value = Number(.95)
		case 3:
			s.Deleted = true
			s.Value = nil
		case 4:
			s.Metric, s.Unit = "body_temperature", "degF"
			s.Value = Number(98.6)
		}
		samples[i] = s
	}
	batch, err := PrepareDeviceBatch(base.SourceDevice, samples)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range samples {
		want, err := s.Normalize()
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		p := &batch.input[i]
		if !bytes.Equal(p.payload, expected) {
			t.Fatalf("payload %d differs", i)
		}
		if s.Value != nil {
			*samples[i].Value = 999
			if p.sample.Value == nil || *p.sample.Value != *want.Value {
				t.Fatalf("caller alias %d", i)
			}
		} else if p.sample.Value != nil {
			t.Fatal("deleted sample gained value")
		}
	}
}

func TestSampleTimestampReusePreservesZone(t *testing.T) {
	_, s := sampleBenchmarkInput()
	for _, ns := range []int{0, 123456789} {
		s.Start = time.Date(2026, 10, 20, 12, 0, 0, ns, time.UTC)
		for _, end := range []time.Time{s.Start, s.Start.In(time.FixedZone("east", 8*3600)), s.Start.Add(time.Nanosecond)} {
			s.End = end
			s.Value = Number(math.Copysign(0, -1))
			got, e := marshalSampleFast(s)
			want, e2 := json.Marshal(s)
			if e != nil || e2 != nil || !bytes.Equal(got, want) {
				t.Fatalf("timestamp mismatch %s %s", got, want)
			}
		}
	}
}

var preparedBatchSink PreparedBatch

func BenchmarkBatchPreparationArena(b *testing.B) {
	_, base := sampleBenchmarkInput()
	for _, n := range []int{1, 128, 200} {
		for _, kind := range []string{"instant", "interval"} {
			input := make([]Sample, n)
			for i := range input {
				s := base
				s.RecordID = strconv.Itoa(i)
				s.Start = s.Start.Add(time.Duration(i) * time.Minute)
				s.End = s.Start
				if kind == "interval" {
					s.Metric, s.Unit, s.Semantics = "steps", "count", "interval"
					s.End = s.Start.Add(time.Minute)
				}
				input[i] = s
			}
			b.Run(fmt.Sprintf("%s/%d", kind, n), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					var err error
					preparedBatchSink, err = PrepareDeviceBatch(base.SourceDevice, input)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
