package rules

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func TestCompactMatchesLegacyResultsAndEvidence(t *testing.T) {
	r := rand.New(rand.NewSource(10923))
	const cases = 5000
	for i := 0; i < cases; i++ {
		p := Default()
		if i%7 == 0 {
			p.Enabled = false
		}
		if i%3 == 0 {
			p.Timezone = "UTC"
		}
		now := time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC).Add(time.Duration(r.Intn(86400)) * time.Second)
		in := Input{Now: now, Baselines: map[string]Baseline{}}
		makeValue := func(k string) *Value {
			v := &Value{Value: float64(r.Intn(280)), Key: k, Source: "data-source", Device: "device-one", At: now.Add(time.Duration(r.Intn(900)-500) * time.Second)}
			if i%6 == 0 {
				v.Source = ""
				v.Device = ""
			}
			return v
		}
		if i%2 == 0 {
			in.Heart = makeValue("heart")
		}
		if i%3 != 0 {
			in.Blood = makeValue("blood")
		}
		if i%4 != 0 {
			in.Battery = makeValue("battery")
		}
		if i%5 != 0 {
			in.Activity = makeValue("activity")
		}
		if i%3 != 1 {
			in.LastReport = now.Add(time.Duration(r.Intn(1000)-120) * time.Second)
		}
		for _, k := range []string{"heart_rate", "blood_pressure"} {
			if i%3 == 2 {
				in.Baselines[k] = Baseline{Count: r.Intn(100), Mean: float64(r.Intn(190)), Deviation: float64(r.Intn(20))}
			}
		}
		want := referenceEvaluate(p, in)
		compact := EvaluateCompact(p, in)
		if len(compact) != len(want) {
			t.Fatalf("case %d: compact count=%d want %d", i, len(compact), len(want))
		}
		for j, c := range compact {
			if c.Kind != want[j].Kind || c.Key != want[j].Key || c.Breach != want[j].Breach || c.Severity != want[j].Severity || c.Required != want[j].Required || !reflect.DeepEqual(c.evidence.asMap(), want[j].Evidence) {
				t.Fatalf("case %d, item %d mismatch", i, j)
			}
			streak := r.Intn(20)
			owner := int64(1 + i)
			typed, err := c.marshalEvidence(streak, owner, now)
			if err != nil {
				t.Fatal(err)
			}
			m := make(map[string]any, len(want[j].Evidence)+5)
			for k, v := range want[j].Evidence {
				m[k] = v
			}
			m["breach"] = c.Breach
			m["streak"] = streak
			m["required"] = c.Required
			m["ownerId"] = owner
			m["at"] = now
			legacy, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			var x, y any
			if err = json.Unmarshal(legacy, &x); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(typed, &y); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(x, y) {
				t.Fatalf("case %d/%d JSON mismatch\nlegacy %s\ntyped %s", i, j, legacy, typed)
			}
		}
	}
}

var compactCandidates []CompactCandidate
var evidenceBytes []byte

func BenchmarkEvaluateCompact(b *testing.B) {
	p, in := Default(), benchmarkInput()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		compactCandidates = EvaluateCompact(p, in)
	}
}
func BenchmarkEvaluateCompactMarshalEvidence(b *testing.B) {
	p, in := Default(), benchmarkInput()
	now := in.Now
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cs := EvaluateCompact(p, in)
		for _, c := range cs {
			out, err := c.marshalEvidence(2, 1234, now)
			if err != nil || len(out) == 0 {
				b.Fatal(err)
			}
			evidenceBytes = out
		}
	}
}
func BenchmarkEvaluateLegacyMarshalEvidence(b *testing.B) {
	p, in := Default(), benchmarkInput()
	now := in.Now
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cs := referenceEvaluate(p, in)
		for _, c := range cs {
			c.Evidence["breach"] = c.Breach
			c.Evidence["streak"] = 2
			c.Evidence["required"] = c.Required
			c.Evidence["ownerId"] = int64(1234)
			c.Evidence["at"] = now
			out, err := json.Marshal(c.Evidence)
			if err != nil || len(out) == 0 {
				b.Fatal(err)
			}
			evidenceBytes = out
		}
	}
}
func TestCompactNoSharedOutput(t *testing.T) {
	p, in := Default(), benchmarkInput()
	a := EvaluateCompact(p, in)
	_, _ = a[0].marshalEvidence(1, 1, in.Now)
	b := EvaluateCompact(p, in)
	if &a[0] == &b[0] {
		t.Fatal("shared result buffer")
	}
	if bytes.Equal([]byte(a[0].Kind), []byte("")) {
		t.Fatal("missing candidate")
	}
}

func BenchmarkEvaluateCompactInto(b *testing.B) {
	p, in := Default(), benchmarkInput()
	var buf [7]CompactCandidate
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		compactCandidates = EvaluateCompactInto(buf[:0], p, in)
	}
}
func TestEvaluateCompactIntoUsesCallerStorage(t *testing.T) {
	p, in := Default(), benchmarkInput()
	var buf [7]CompactCandidate
	out := EvaluateCompactInto(buf[:0], p, in)
	if len(out) != 7 || &out[0] != &buf[0] {
		t.Fatalf("expected exactly seven in-place candidates, got %d", len(out))
	}
	p.Enabled = false
	if got := EvaluateCompactInto(buf[:0], p, in); len(got) != 0 {
		t.Fatal("disabled policy must emit nothing")
	}
}

func BenchmarkEvaluateCompactIntoMarshalEvidence(b *testing.B) {
	p, in := Default(), benchmarkInput()
	var buf [7]CompactCandidate
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, c := range EvaluateCompactInto(buf[:0], p, in) {
			out, err := c.marshalEvidence(2, 1234, in.Now)
			if err != nil || len(out) == 0 {
				b.Fatal(err)
			}
			evidenceBytes = out
		}
	}
}
func TestCompactSnapshotDoesNotReferenceMutableInputs(t *testing.T) {
	p, in := Default(), benchmarkInput()
	old := EvaluateCompact(p, in)
	in.Heart.Value = 1
	in.Heart.At = in.Now.Add(-7 * time.Minute)
	if old[0].evidence.value != 100 || !old[0].evidence.observedAt.Equal(in.Now) {
		t.Fatal("evidence retained mutable Value pointer")
	}
}

// EvaluateCompact keeps the allocating baseline available to equivalence tests
// and benchmarks. Production evaluates into caller-owned storage.
func EvaluateCompact(p Policy, in Input) []CompactCandidate {
	if !p.Enabled {
		return nil
	}
	return EvaluateCompactInto(make([]CompactCandidate, 0, 7), p, in)
}
