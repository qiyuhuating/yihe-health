package health

import (
	"runtime"
	"testing"
)

// CPU-only normalization, JSON encoding and identity hash; independent workers
// never share mutable sample buffers.
func BenchmarkSamplePrepFastParallel(b *testing.B) {
	o, s := sampleBenchmarkInput()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		used := 0
		for pb.Next() {
			n, err := s.Normalize()
			if err != nil {
				panic(err)
			}
			payload, err := marshalSampleFast(n)
			if err != nil {
				panic(err)
			}
			id := identityDigest(o, n)
			used += len(payload) + len(id)
		}
		runtime.KeepAlive(used)
	})
}
