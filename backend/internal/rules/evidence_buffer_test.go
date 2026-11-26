package rules

import (
	"bytes"
	"runtime"
	"testing"
)

func TestAppendEvidenceFastReusableBuffer(t *testing.T) {
	p, in := Default(), benchmarkInput()
	var candidates [7]CompactCandidate
	var buffer [512]byte
	for _, c := range EvaluateCompactInto(candidates[:0], p, in) {
		first, err := c.appendEvidenceFast(buffer[:0], 2, 1234, in.Now)
		if err != nil {
			t.Fatal(err)
		}
		old, err := c.marshalEvidence(2, 1234, in.Now)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, old) {
			t.Fatalf("%s differs: %s vs %s", c.Kind, first, old)
		}
		if len(first) > len(buffer) {
			t.Fatalf("unexpected large evidence %d", len(first))
		}
		if len(first) > 0 && &first[0] != &buffer[0] {
			t.Fatalf("%s did not reuse buffer", c.Kind)
		}
		second, err := c.appendEvidenceFast(buffer[:0], 3, 9876, in.Now)
		if err != nil {
			t.Fatal(err)
		}
		canonical, err := c.marshalEvidence(3, 9876, in.Now)
		if err != nil || !bytes.Equal(second, canonical) {
			t.Fatalf("%s overwrite differs: %s vs %s err %v", c.Kind, second, canonical, err)
		}
	}
}

var retainedEvidenceByteCount int

func BenchmarkEvaluateFastReusableBuffer(b *testing.B) {
	p, in := Default(), benchmarkInput()
	b.ReportAllocs()
	processed := 0
	for i := 0; i < b.N; i++ {
		var candidates [7]CompactCandidate
		var evidence [512]byte
		for _, c := range EvaluateCompactInto(candidates[:0], p, in) {
			body, err := c.appendEvidenceFast(evidence[:0], 2, 1234, in.Now)
			if err != nil {
				b.Fatal(err)
			}
			processed += len(body)
		}
	}
	retainedEvidenceByteCount = processed
}

func BenchmarkEvaluateFastReusableBufferParallel(b *testing.B) {
	p, in := Default(), benchmarkInput()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var candidates [7]CompactCandidate
		var evidence [512]byte
		used := 0
		for pb.Next() {
			for _, c := range EvaluateCompactInto(candidates[:0], p, in) {
				body, err := c.appendEvidenceFast(evidence[:0], 2, 1234, in.Now)
				if err != nil {
					panic(err)
				}
				used += len(body)
			}
		}
		runtime.KeepAlive(used)
	})
}
