package rules

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func TestFastEvidenceEquivalentToCanonical(t *testing.T) {
	r := rand.New(rand.NewSource(35672))
	p, in := Default(), benchmarkInput()
	for i := 0; i < 6000; i++ {
		source := []string{"", "unicode手表", "quote\"", "\u2028", "<>&", "bad\nvalue", "\\", "watch"}
		in.Heart.Source = source[i%len(source)]
		in.Heart.Value = []float64{0, 1e-9, 1e-7, 1e21, 1e35, 0.000001, 0.0000009999, math.MaxFloat64, float64(r.Intn(200))}[i%9]
		in.Blood.Value = in.Heart.Value
		now := in.Now.Add(time.Duration(r.Intn(3600)) * time.Second)
		for _, c := range EvaluateCompact(p, in) {
			fast, err := c.appendEvidenceFast(nil, i%20, int64(i+1), now)
			if err != nil {
				t.Fatal(err)
			}
			old, err := c.marshalEvidence(i%20, int64(i+1), now)
			if err != nil {
				t.Fatal(err)
			}
			var x, y any
			if err = json.Unmarshal(fast, &x); err != nil {
				t.Fatalf("invalid fast JSON %s: %v", fast, err)
			}
			if err = json.Unmarshal(old, &y); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(x, y) {
				t.Fatalf("case %d %s\nfast: %s\nold: %s", i, c.Kind, fast, old)
			}
		}
	}
}
func BenchmarkEvaluateCompactIntoFastJSON(b *testing.B) {
	p, in := Default(), benchmarkInput()
	var buf [7]CompactCandidate
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, c := range EvaluateCompactInto(buf[:0], p, in) {
			out, err := c.appendEvidenceFast(nil, 2, 1234, in.Now)
			if err != nil || len(out) == 0 {
				b.Fatal(err)
			}
			evidenceBytes = out
		}
	}
}
