package health

import (
	"testing"
	"time"
)

func TestContinuousStepExactIntegerBounds(t *testing.T) {
	at := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	for _, semantics := range []string{"daily_total", "interval"} {
		s := Sample{RecordID: "steps", Source: "watch", SourceDevice: "A", Metric: "steps", Unit: "count", Value: Number(9007199254740992), Start: at, End: at.Add(time.Hour), Semantics: semantics, Version: 1}
		if _, err := s.Normalize(); err == nil {
			t.Errorf("%s accepted steps beyond the exact JSON integer range", semantics)
		}
		s.Value = Number(9007199254740991)
		if _, err := s.Normalize(); err != nil {
			t.Errorf("valid upper bound rejected: %v", err)
		}
	}
}

func TestContinuousDailyStepTotalOverflow(t *testing.T) {
	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	s := Sample{Source: "watch", SourceDevice: "A", Metric: "steps", Unit: "count", Value: Number(9007199254740991), Start: from, End: from, Semantics: "daily_total"}
	next := s
	next.Start, next.End, next.Value = from.AddDate(0, 0, 1), from.AddDate(0, 0, 1), Number(1)
	if out, err := Aggregate([]Sample{s, next}, from, from.AddDate(0, 0, 2)); err == nil {
		t.Fatalf("inexact multi-day total returned as valid: %+v", out)
	}
}
