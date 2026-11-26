// Package health defines sampling semantics shared by device uploads and imports.
package health

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type Sample struct {
	RecordID      string            `json:"recordId"`
	Source        string            `json:"source"`
	SourceDevice  string            `json:"sourceDevice"`
	SourceVersion string            `json:"sourceVersion,omitempty"`
	Metric        string            `json:"metric"`
	Unit          string            `json:"unit"`
	Value         *float64          `json:"value"`
	Start         time.Time         `json:"start"`
	End           time.Time         `json:"end"`
	Semantics     string            `json:"semantics"`
	GroupID       string            `json:"groupId,omitempty"`
	Stage         string            `json:"stage,omitempty"`
	TimeBasis     string            `json:"timeBasis,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Version       int64             `json:"version"`
	Deleted       bool              `json:"deleted,omitempty"`
}

func Number(v float64) *float64 { return &v }

// InWindow uses exact payload times. Microsecond SQL indexes only select
// candidates; instants belong to [from,to), intervals must overlap that window.
func (s Sample) InWindow(from, to time.Time) bool {
	if s.Semantics == "interval" {
		return s.Start.Before(to) && s.End.After(from)
	}
	return !s.Start.Before(from) && s.Start.Before(to)
}

func (s Sample) Normalize() (Sample, error) {
	if err := s.validateIdentity(); err != nil {
		return s, err
	}
	unit, semantics, err := s.metricContract()
	if err != nil {
		return s, err
	}

	if s.Semantics != "" && s.Semantics != semantics {
		return s, fmt.Errorf("metric semantics mismatch")
	}
	if semantics == "instant" && !s.Start.Equal(s.End) {
		return s, fmt.Errorf("instant sample must have equal start and end")
	}
	if semantics == "interval" && !s.End.After(s.Start) {
		return s, fmt.Errorf("interval must have positive duration")
	}
	return s.normalizeMeasurement(unit, semantics)
}

func (s *Sample) validateIdentity() error {
	if len(s.Metadata) > 64 {
		return fmt.Errorf("too many metadata fields")
	}
	for k, v := range s.Metadata {
		if len(k) > 256 || len(v) > 4096 {
			return fmt.Errorf("metadata field too large")
		}
	}
	if s.RecordID == "" || len(s.RecordID) > 256 || s.Source == "" || len(s.Source) > 256 || s.SourceDevice == "" || len(s.SourceDevice) > 4096 || len(s.SourceVersion) > 128 || len(s.GroupID) > 256 {
		return fmt.Errorf("recordId, source and sourceDevice are required and must fit their limits")
	}
	if strings.TrimSpace(s.RecordID) != s.RecordID || strings.TrimSpace(s.Source) != s.Source {
		return fmt.Errorf("recordId and source must not have surrounding whitespace")
	}
	if s.Start.IsZero() || s.End.IsZero() || s.Start.Year() < 1 || s.End.Year() > 9999 || s.End.Before(s.Start) || s.Version < 1 {
		return fmt.Errorf("valid start, end and positive version are required")
	}
	if s.TimeBasis != "" && s.TimeBasis != "legacy-unzoned" {
		return fmt.Errorf("unknown time basis")
	}
	return nil
}

func (s *Sample) metricContract() (string, string, error) {
	unit, semantics := "", "instant"
	switch s.Metric {
	case "heart_rate":
		unit = "count/min"
	case "blood_pressure_systolic", "blood_pressure_diastolic", "legacy_blood_pressure":
		unit = "mmHg"
	case "oxygen_saturation":
		unit = "%"
	case "body_temperature":
		unit = "degC"
	case "body_mass":
		unit = "kg"
	case "steps":
		unit = "count"
		if s.Semantics != "interval" && s.Semantics != "daily_total" {
			return "", "", fmt.Errorf("steps must declare interval or daily_total")
		}
		semantics = s.Semantics
	case "sleep_duration":
		unit = "min"
		semantics = "daily_total"
	case "sleep_stage":
		unit = "min"
		semantics = "interval"
		switch s.Stage {
		case "in_bed", "asleep", "core", "deep", "rem", "awake":
		default:
			return "", "", fmt.Errorf("unknown sleep stage")
		}
	default:
		return "", "", fmt.Errorf("unsupported metric %q", s.Metric)
	}
	return unit, semantics, nil
}

func (s *Sample) normalizeMeasurement(unit, semantics string) (Sample, error) {
	s.Semantics = semantics
	if s.Value == nil {
		if s.Deleted {
			s.Unit = unit
			return *s, nil
		}
		return *s, fmt.Errorf("missing measurement")
	}
	v := *s.Value
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return *s, fmt.Errorf("measurement must be finite and nonnegative")
	}
	if s.Metric == "body_temperature" && s.Unit == "degF" {
		v = (v - 32) * 5 / 9
		s.Unit = unit
	}
	if s.Metric == "body_mass" && s.Unit == "lb" {
		v *= 0.45359237
		s.Unit = unit
	}
	if s.Metric == "sleep_duration" && s.Unit == "h" {
		v *= 60
		s.Unit = unit
	}
	if s.Metric == "oxygen_saturation" && s.Unit == "fraction" {
		v *= 100
		s.Unit = unit
	}
	if s.Unit != unit {
		return *s, fmt.Errorf("unsupported unit %q for %s", s.Unit, s.Metric)
	}
	if s.Metric == "sleep_stage" {
		v = s.End.Sub(s.Start).Minutes()
	}
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return *s, fmt.Errorf("normalized measurement must be finite and nonnegative")
	}
	if s.Metric == "steps" && (v != math.Trunc(v) || v > 9007199254740991) {
		return *s, fmt.Errorf("steps must be an exact nonnegative JSON integer")
	}
	if s.Metric == "oxygen_saturation" && v > 100 {
		return *s, fmt.Errorf("oxygen saturation exceeds 100 percent")
	}
	s.Value = Number(v)
	return *s, nil
}
