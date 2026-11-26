package fence

import (
	"math"
	"math/rand"
	"testing"
)

func regularFence(n int) []Point {
	p := make([]Point, n+1)
	for i := 0; i < n; i++ {
		a := float64(i) * 2 * math.Pi / float64(n)
		p[i] = Point{120 + 0.01*math.Cos(a), 30 + 0.01*math.Sin(a)}
	}
	p[n] = p[0]
	return p
}

func TestWithinBoundaryExactThreshold(t *testing.T) {
	rng := rand.New(rand.NewSource(20261126))
	for _, n := range []int{4, 32, 256} {
		p := regularFence(n)
		if err := Validate(p); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			q := Point{120 + .04*(rng.Float64()-.5), 30 + .04*(rng.Float64()-.5)}
			if i < n {
				q = p[i]
			}
			d := BoundaryDistance(p, q)
			for _, r := range []float64{0, 5, 30, math.Nextafter(d, math.Inf(-1)), d, math.Nextafter(d, math.Inf(1))} {
				if got, want := WithinBoundary(p, q, r), d <= r; got != want {
					t.Fatalf("n=%d point=%v radius=%.17g distance=%.17g got=%v", n, q, r, d, got)
				}
			}
		}
	}
}

var fencePredicate bool

func BenchmarkBoundaryPredicate(b *testing.B) {
	for _, tc := range []struct {
		name string
		q    Point
	}{
		{"near_first", Point{120.01, 30}},
		{"near_middle", Point{119.99, 30}},
		{"far", Point{120, 30}},
	} {
		p := regularFence(256)
		b.Run(tc.name+"/legacy", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				fencePredicate = BoundaryDistance(p, tc.q) <= 10
			}
		})
		b.Run(tc.name+"/early", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				fencePredicate = WithinBoundary(p, tc.q, 10)
			}
		})
	}
}
