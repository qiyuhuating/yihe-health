package rules

import (
	"runtime"
	"testing"
)

// Measures CPU-only rule evaluation and evidence serialization under contention;
// no SQL, HTTP, notification delivery or network I/O is included.
func BenchmarkEvaluateFastParallel(b *testing.B) {
	p, in := Default(), benchmarkInput()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		var storage [7]CompactCandidate
		used := 0
		for pb.Next() {
			for _, c := range EvaluateCompactInto(storage[:0], p, in) {
				body, err := c.appendEvidenceFast(nil, 2, 1234, in.Now)
				if err != nil {
					panic(err)
				}
				used += len(body)
			}
		}
		runtime.KeepAlive(used)
	})
}
