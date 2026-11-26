package fence

import (
	"math"
	"testing"
)

func reviewOldContains(p []Point, q Point) bool {
	inside := false
	for i := 0; i < len(p)-1; i++ {
		a, b := p[i], p[i+1]
		if on(a, b, q) {
			return true
		}
		if (a[1] > q[1]) != (b[1] > q[1]) && q[0] < (b[0]-a[0])*(q[1]-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
	}
	return inside
}
func TestAlgorithmFenceDifferential(t *testing.T) {
	polygons := [][]Point{{{0, 0}, {.01, 0}, {.01, .01}, {0, .01}, {0, 0}}, {{0, 0}, {.01, 0}, {.01, .01}, {.005, .005}, {0, .01}, {0, 0}}}
	for _, p := range polygons {
		if err := Validate(p); err != nil {
			t.Fatal(err)
		}
		for x := -10; x <= 110; x++ {
			for y := -10; y <= 110; y++ {
				q := Point{float64(x) * .0001, float64(y) * .0001}
				if Contains(p, q) != reviewOldContains(p, q) {
					t.Fatalf("difference at %v", q)
				}
			}
		}
	}
}
func TestAlgorithmDegenerateBoundaryDistance(t *testing.T) {
	p := []Point{{0, 0}, {0, 0}, {.01, 0}, {.01, .01}, {0, 0}}
	d := BoundaryDistance(p, Point{.005, .001})
	if math.IsNaN(d) || math.IsInf(d, 0) || d <= 0 {
		t.Fatalf("nonfinite or invalid distance %v", d)
	}
}
