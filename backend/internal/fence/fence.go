// Package fence implements the Yihe WGS84 fence contract, not an APK algorithm.
package fence

import (
	"encoding/json"
	"fmt"
	"math"
	"time"
)

type Point [2]float64 // longitude, latitude

func (p *Point) UnmarshalJSON(b []byte) error {
	var values []*float64
	if err := json.Unmarshal(b, &values); err != nil {
		return err
	}
	if len(values) != 2 || values[0] == nil || values[1] == nil {
		return fmt.Errorf("坐标必须恰好包含经度、纬度两个数值")
	}
	*p = Point{*values[0], *values[1]}
	return nil
}

func ValidPoint(p Point) bool {
	return !math.IsNaN(p[0]) && !math.IsNaN(p[1]) && !math.IsInf(p[0], 0) && !math.IsInf(p[1], 0) && p[0] >= -180 && p[0] <= 180 && p[1] >= -85 && p[1] <= 85
}
func cross(a, b, p Point) float64 { return (b[0]-a[0])*(p[1]-a[1]) - (b[1]-a[1])*(p[0]-a[0]) }
func on(a, b, p Point) bool {
	return math.Abs(cross(a, b, p)) < 1e-12 && p[0] >= math.Min(a[0], b[0])-1e-12 && p[0] <= math.Max(a[0], b[0])+1e-12 && p[1] >= math.Min(a[1], b[1])-1e-12 && p[1] <= math.Max(a[1], b[1])+1e-12
}
func intersects(a, b, c, d Point) bool {
	return on(a, b, c) || on(a, b, d) || on(c, d, a) || on(c, d, b) || (cross(a, b, c)*cross(a, b, d) < 0 && cross(c, d, a)*cross(c, d, b) < 0)
}
func Validate(p []Point) error {
	if len(p) < 4 || len(p) > 257 || p[0] != p[len(p)-1] {
		return fmt.Errorf("围栏须为首尾闭合的 3–256 个顶点")
	}
	lo, hi, area := p[0], p[0], 0.0
	for i, q := range p {
		if !ValidPoint(q) {
			return fmt.Errorf("无效 WGS84 坐标（纬度限 ±85）")
		}
		lo[0] = math.Min(lo[0], q[0])
		lo[1] = math.Min(lo[1], q[1])
		hi[0] = math.Max(hi[0], q[0])
		hi[1] = math.Max(hi[1], q[1])
		if i == len(p)-1 {
			break
		}
		if q == p[i+1] {
			return fmt.Errorf("相邻顶点重复")
		}
		area += q[0]*p[i+1][1] - p[i+1][0]*q[1]
		for j := i + 2; j < len(p)-1; j++ {
			if i == 0 && j == len(p)-2 {
				continue
			}
			if intersects(q, p[i+1], p[j], p[j+1]) {
				return fmt.Errorf("围栏边不能自交")
			}
		}
	}
	// A local planar fence, deliberately excluding date-line/polar/large polygons.
	if hi[0]-lo[0] > 1 || hi[1]-lo[1] > 1 || math.Abs(area) < 1e-12 {
		return fmt.Errorf("围栏须为非退化的局部多边形（跨度不超过 1 度）")
	}
	return nil
}
func Contains(p []Point, q Point) bool {
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

// BoundaryDistance returns a local equirectangular distance in metres.
func BoundaryDistance(p []Point, q Point) float64 {
	xscale := 111195 * math.Cos(q[1]*math.Pi/180)
	best := math.Inf(1)
	for i := 0; i < len(p)-1; i++ {
		ax, ay := (p[i][0]-q[0])*xscale, (p[i][1]-q[1])*111195
		bx, by := (p[i+1][0]-q[0])*xscale, (p[i+1][1]-q[1])*111195
		dx, dy := bx-ax, by-ay
		if dx*dx+dy*dy < 1e-12 {
			best = math.Min(best, math.Hypot(ax, ay))
			continue
		}
		t := math.Max(0, math.Min(1, -(ax*dx+ay*dy)/(dx*dx+dy*dy)))
		best = math.Min(best, math.Hypot(ax+t*dx, ay+t*dy))
	}
	return best
}
func Active(second, begin, end int) bool {
	if begin < end {
		return second >= begin && second < end
	}
	return second >= begin || second < end
}

type State struct {
	Initialized bool      `json:"initialized"`
	Inside      bool      `json:"inside"`
	Candidate   bool      `json:"candidate"`
	Count       int       `json:"count"`
	Since       time.Time `json:"since"`
	Last        time.Time `json:"last"`
}

func (s *State) Observe(inside bool, at time.Time, minPoints int, dwell time.Duration) string {
	if !at.After(s.Last) {
		return ""
	}
	if !s.Last.IsZero() && at.Sub(s.Last) > 5*time.Minute {
		s.Count = 0
		s.Since = time.Time{}
	}
	s.Last = at
	if !s.Initialized {
		s.Initialized = true
		s.Inside = inside
		return ""
	}
	if inside == s.Inside {
		s.Count = 0
		s.Since = time.Time{}
		return ""
	}
	if s.Count == 0 || s.Candidate != inside {
		s.Candidate = inside
		s.Count = 0
		s.Since = at
	}
	s.Count++
	if s.Count < minPoints || at.Sub(s.Since) < dwell {
		return ""
	}
	s.Inside = inside
	s.Count = 0
	s.Since = time.Time{}
	if inside {
		return "entry"
	}
	return "exit"
}
