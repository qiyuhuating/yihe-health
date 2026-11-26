package fence

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCoordinateArity(t *testing.T) {
	for _, raw := range []string{`[120]`, `[120,30,99]`, `null`, `[null,30]`, `[120,null]`, `[null,null]`} {
		var p Point
		if json.Unmarshal([]byte(raw), &p) == nil {
			t.Fatal("accepted malformed coordinate", raw)
		}
	}
}

func TestGeometryAndDebounce(t *testing.T) {
	p := []Point{{120, 30}, {120.01, 30}, {120.01, 30.01}, {120, 30.01}, {120, 30}}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	if !Contains(p, Point{120.005, 30.005}) || !Contains(p, p[0]) || Contains(p, Point{121, 30}) {
		t.Fatal("containment/boundary")
	}
	if err := Validate([]Point{{0, 0}, {1, 1}, {0, 1}, {1, 0}, {0, 0}}); err == nil {
		t.Fatal("self intersection")
	}
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	s := State{}
	if s.Observe(true, now, 2, 10*time.Second) != "" {
		t.Fatal("initial point is not an entry event")
	}
	if s.Observe(false, now.Add(time.Second), 2, 10*time.Second) != "" {
		t.Fatal("single drift point")
	}
	if s.Observe(false, now.Add(2*time.Second), 2, 10*time.Second) != "" {
		t.Fatal("insufficient duration")
	}
	if s.Observe(false, now.Add(11*time.Second), 2, 10*time.Second) != "exit" {
		t.Fatal("missing exit")
	}
	if s.Observe(true, now.Add(3*time.Second), 2, 10*time.Second) != "" {
		t.Fatal("accepted old point")
	}
	if s.Observe(false, now.Add(12*time.Second), 2, 10*time.Second) != "" {
		t.Fatal("duplicate exit")
	}
}

func TestScheduleAcrossMidnight(t *testing.T) {
	if !Active(23*3600, 22*3600, 6*3600) || !Active(3600, 22*3600, 6*3600) || Active(12*3600, 22*3600, 6*3600) {
		t.Fatal("overnight")
	}
	if !Active(86399, 0, 86400) || Active(6*3600, 22*3600, 6*3600) {
		t.Fatal("half-open range")
	}
}
