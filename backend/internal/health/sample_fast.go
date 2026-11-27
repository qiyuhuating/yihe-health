package health

import (
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"time"

	"yihe-care/internal/jsonscan"
)

// marshalSampleFast matches encoding/json's struct-field ordering and bytes
// for the common, fully normalized, scalar health sample. Uncommon or
// untrusted representations always use encoding/json for exact compatibility.
func marshalSampleFast(s Sample) ([]byte, error) {
	if s.Value == nil || !finiteSample(*s.Value) || len(s.Metadata) > 8 || s.Deleted ||
		!safeSampleString(s.RecordID) || !safeSampleString(s.Source) || !safeSampleString(s.SourceDevice) ||
		!safeSampleString(s.Metric) || !safeSampleString(s.Unit) || !safeSampleString(s.Semantics) ||
		(s.SourceVersion != "" && !safeSampleString(s.SourceVersion)) || (s.GroupID != "" && !safeSampleString(s.GroupID)) || (s.Stage != "" && !safeSampleString(s.Stage)) || (s.TimeBasis != "" && !safeSampleString(s.TimeBasis)) ||
		!safeSampleTime(s.Start) || (s.Start != s.End && !safeSampleTime(s.End)) {
		return json.Marshal(s)
	}
	var keys [8]string
	count, extra := 0, 0
	for key, value := range s.Metadata {
		if !safeSampleString(key) || !safeSampleString(value) {
			return json.Marshal(s)
		}
		keys[count] = key
		count++
		extra += len(key) + len(value) + 6
	}
	if count > 1 {
		slices.Sort(keys[:count])
	}
	if count > 0 {
		extra += len(`,"metadata":{}`)
	}
	if s.SourceVersion != "" {
		extra += len(s.SourceVersion) + len(`,"sourceVersion":""`)
	}
	if s.GroupID != "" {
		extra += len(s.GroupID) + len(`,"groupId":""`)
	}
	if s.Stage != "" {
		extra += len(s.Stage) + len(`,"stage":""`)
	}
	if s.TimeBasis != "" {
		extra += len(s.TimeBasis) + len(`,"timeBasis":""`)
	}

	// Lengths of strings vary widely across devices. Allocate only one output
	// buffer; a bounded reserve covers common timestamps and measurements.
	// Append grows safely for unusually long RFC3339Nano/float representations.
	b := make([]byte, 0, 208+extra+len(s.RecordID)+len(s.Source)+len(s.SourceDevice)+len(s.Metric)+len(s.Unit)+len(s.Semantics))
	b = append(b, `{"recordId":"`...)
	b = append(b, s.RecordID...)
	b = append(b, `","source":"`...)
	b = append(b, s.Source...)
	b = append(b, `","sourceDevice":"`...)
	b = append(b, s.SourceDevice...)
	b = append(b, '"')
	if s.SourceVersion != "" {
		b = appendOptionalSampleString(b, "sourceVersion", s.SourceVersion)
	}
	b = append(b, `,"metric":"`...)
	b = append(b, s.Metric...)
	b = append(b, `","unit":"`...)
	b = append(b, s.Unit...)
	b = append(b, `","value":`...)
	b = appendFloatSample(b, *s.Value)
	b = append(b, `,"start":"`...)
	startOffset := len(b)
	b = s.Start.AppendFormat(b, time.RFC3339Nano)
	startEnd := len(b)
	b = append(b, `","end":"`...)
	if s.Start == s.End {
		b = append(b, b[startOffset:startEnd]...)
	} else {
		b = s.End.AppendFormat(b, time.RFC3339Nano)
	}
	b = append(b, `","semantics":"`...)
	b = append(b, s.Semantics...)
	b = append(b, '"')
	if s.GroupID != "" {
		b = appendOptionalSampleString(b, "groupId", s.GroupID)
	}
	if s.Stage != "" {
		b = appendOptionalSampleString(b, "stage", s.Stage)
	}
	if s.TimeBasis != "" {
		b = appendOptionalSampleString(b, "timeBasis", s.TimeBasis)
	}
	if count > 0 {
		b = append(b, `,"metadata":{`...)
		for i, key := range keys[:count] {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, '"')
			b = append(b, key...)
			b = append(b, `":"`...)
			b = append(b, s.Metadata[key]...)
			b = append(b, '"')
		}
		b = append(b, '}')
	}
	b = append(b, `,"version":`...)
	b = strconv.AppendInt(b, s.Version, 10)
	return append(b, '}'), nil
}

func appendOptionalSampleString(b []byte, key, value string) []byte {
	if value == "" {
		return b
	}
	b = append(b, `,"`...)
	b = append(b, key...)
	b = append(b, `":"`...)
	b = append(b, value...)
	return append(b, '"')
}

func safeSampleString(s string) bool { return jsonscan.Safe(s) }
func safeSampleTime(t time.Time) bool {
	year := t.Year()
	_, offset := t.Zone()
	return year >= 0 && year <= 9999 && offset%60 == 0 && offset > -24*3600 && offset < 24*3600
}
func finiteSample(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
func appendFloatSample(b []byte, v float64) []byte {
	if v != 0 && v > -1e15 && v < 1e15 && v == math.Trunc(v) {
		return strconv.AppendInt(b, int64(v), 10)
	}
	format := byte('f')
	abs := math.Abs(v)
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b = strconv.AppendFloat(b, v, format, -1, 64)
	if format == 'e' {
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}
