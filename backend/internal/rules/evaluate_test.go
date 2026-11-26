package rules

import (
	"testing"
	"time"
)

func TestNightBaselineAndMissingData(t *testing.T) {
	p := Default()
	now := time.Date(2026, 10, 20, 15, 0, 0, 0, time.UTC)
	in := Input{Now: now, LastReport: now, Heart: &Value{100, "new", "watch", "one", now}, Baselines: map[string]Baseline{"heart_rate": {20, 70, 2}}, Activity: &Value{250, "steps", "watch", "one", now}}
	got := EvaluateCompact(p, in)
	baseline, night := false, false
	for _, c := range got {
		if c.Kind == "baseline_heart_rate" {
			baseline = c.Breach && c.Required == 3
		}
		if c.Kind == "night_activity" {
			night = c.Breach
		}
		if c.Kind == "heart_rate" && c.Breach {
			t.Fatal("baseline deviation is not the fixed threshold")
		}
	}
	if !baseline || !night {
		t.Fatal(got)
	}
	in.Baselines["heart_rate"] = Baseline{19, 70, 2}
	for _, c := range EvaluateCompact(p, in) {
		if c.Kind == "baseline_heart_rate" {
			t.Fatal("insufficient baseline")
		}
	}
	in.Heart.At = now.Add(-6 * time.Minute)
	for _, c := range EvaluateCompact(p, in) {
		if c.Kind == "heart_rate" {
			t.Fatal("stale observation")
		}
	}
	if got := EvaluateCompact(p, Input{Now: now}); len(got) != 0 {
		t.Fatal("missing measurements became zero alarms", got)
	}
}
