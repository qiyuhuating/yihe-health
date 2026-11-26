package health

import (
	"testing"
	"time"
)

func TestInstantAndDeletedIntervalBounds(t *testing.T) {
	at := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	s := Sample{RecordID: "point", Source: "watch", SourceDevice: "watch-1", Metric: "heart_rate", Unit: "count/min", Start: at, End: at.Add(time.Minute), Value: Number(72), Version: 1}
	if _, err := s.Normalize(); err == nil {
		t.Fatal("instant sample accepted a time interval")
	}
	s.Metric, s.Semantics, s.Unit = "steps", "interval", "count"
	s.Deleted, s.Value, s.End = true, nil, at
	if _, err := s.Normalize(); err == nil {
		t.Fatal("deleted interval accepted invalid bounds")
	}
}
