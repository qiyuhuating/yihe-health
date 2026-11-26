package handlers

import (
	"bytes"
	stdjson "encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	gojson "github.com/goccy/go-json"
	"yihe-care/internal/health"
)

func storedDecoderSample(i int) health.Sample {
	at := time.Date(2026, 10, 20, 12, i%60, i%60, i, time.UTC)
	// Deterministic finite bit patterns exercise decimal rounding across exponents.
	value := math.Float64frombits((uint64(i+1) * 0x9e3779b97f4a7c15) & 0x7fefffffffffffff)
	s := health.Sample{
		RecordID: fmt.Sprintf("sample-%d", i), Source: "watch", SourceDevice: "WATCH-123456",
		Metric: "heart_rate", Unit: "count/min", Value: health.Number(value),
		Start: at, End: at, Semantics: "instant", Version: 1,
	}
	if i%3 == 0 {
		s.SourceVersion, s.GroupID, s.TimeBasis = "v2.1.0", "group-123", "legacy-unzoned"
		s.Metadata = map[string]string{"设备": "A", "note": "<tag>& \"quoted\"\n\\ \u2028"}
	}
	if i%5 == 0 {
		s.Metric, s.Unit, s.Semantics, s.Stage = "sleep_stage", "min", "interval", "deep"
		s.End, s.Value = at.Add(time.Hour), health.Number(60)
	}
	if i%7 == 0 {
		s.Deleted, s.Value = true, nil
	}
	return s
}

func storedDecoderJSON(t *testing.T, s health.Sample) []byte {
	t.Helper()
	b, err := stdjson.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func storedDecoderCorpus(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for i := 0; i < 256; i++ {
		out[fmt.Sprintf("stored_%03d", i)] = storedDecoderJSON(t, storedDecoderSample(i))
	}
	edges := map[string]string{
		"empty_object":        `{}`,
		"null":                `null`,
		"whitespace":          " \r\n\t{\"version\":1} \n",
		"empty_input":         ``,
		"trailing_object":     `{} {}`,
		"trailing_garbage":    `{}x`,
		"array":               `[]`,
		"boolean":             `true`,
		"string":              `"sample"`,
		"number":              `1`,
		"int_max":             `{"version":9223372036854775807}`,
		"int_min":             `{"version":-9223372036854775808}`,
		"int_overflow":        `{"version":9223372036854775808}`,
		"int_underflow":       `{"version":-9223372036854775809}`,
		"int_fraction":        `{"version":1.5}`,
		"int_exponent":        `{"version":1e3}`,
		"int_string":          `{"version":"1"}`,
		"int_null":            `{"version":null}`,
		"float_max":           `{"value":1.7976931348623157e308}`,
		"float_overflow":      `{"value":1e309}`,
		"float_underflow":     `{"value":1e-1000}`,
		"float_subnormal":     `{"value":5e-324}`,
		"float_negative_zero": `{"value":-0}`,
		"float_precision":     `{"value":9007199254740993}`,
		"float_string":        `{"value":"72"}`,
		"float_nan":           `{"value":NaN}`,
		"float_inf":           `{"value":Infinity}`,
		"duplicate_scalar":    `{"recordId":"old","recordId":"new","version":1,"version":2}`,
		"duplicate_null":      `{"value":72,"value":null}`,
		"duplicate_map":       `{"metadata":{"a":"one"},"metadata":{"b":"two","a":"last"}}`,
		"duplicate_map_null":  `{"metadata":{"a":"one"},"metadata":null}`,
		"casefold":            `{"RECORDID":"upper","recordId":"lower","VERSION":2}`,
		"escaped_key":         `{"record\u0049d":"record","meta\u0064ata":{"\u4e2d":"\u6587"}}`,
		"unicode":             `{"recordId":"中文😀","metadata":{"\ud83d\ude00":"\u2028\u2029"}}`,
		"surrogate_high":      `{"recordId":"\ud800"}`,
		"surrogate_low":       `{"recordId":"\udfff"}`,
		"control_unescaped":   "{\"recordId\":\"a\nb\"}",
		"escape_invalid":      `{"recordId":"\x20"}`,
		"metadata_null":       `{"metadata":null}`,
		"metadata_empty":      `{"metadata":{}}`,
		"metadata_null_value": `{"metadata":{"a":null}}`,
		"metadata_number":     `{"metadata":{"a":1}}`,
		"metadata_nested":     `{"metadata":{"a":{}}}`,
		"metadata_array":      `{"metadata":[]}`,
		"time_null":           `{"start":null,"end":null}`,
		"time_nano_offset":    `{"start":"2026-10-20T12:00:00.123456789+08:00","end":"2026-10-20T12:00:00.123456789+08:00"}`,
		"time_min":            `{"start":"0001-01-01T00:00:00Z"}`,
		"time_max":            `{"end":"9999-12-31T23:59:59.999999999Z"}`,
		"time_leap":           `{"start":"2024-02-29T12:00:00Z"}`,
		"time_invalid_date":   `{"start":"2026-02-29T12:00:00Z"}`,
		"time_space":          `{"start":"2026-10-20 12:00:00Z"}`,
		"time_lowercase":      `{"start":"2026-10-20t12:00:00z"}`,
		"time_number":         `{"start":123}`,
		"time_bad_offset":     `{"start":"2026-10-20T12:00:00+25:00"}`,
		"unknown_valid":       `{"unknown":{"a":[null,true,1,"x"]},"version":2}`,
		"unknown_invalid":     `{"unknown":[1,},"version":2}`,
		"truncated":           `{"metadata":{"a":"value"}`,
		"deleted_null":        `{"deleted":null}`,
		"deleted_wrong":       `{"deleted":1}`,
	}
	for name, data := range edges {
		out[name] = []byte(data)
	}
	out["invalid_utf8"] = []byte("{\"recordId\":\"\xff\xfe\"}")
	max := storedDecoderSample(3)
	max.RecordID, max.Source, max.SourceDevice = strings.Repeat("r", 256), strings.Repeat("s", 256), strings.Repeat("d", 4096)
	max.SourceVersion, max.GroupID = strings.Repeat("v", 128), strings.Repeat("g", 256)
	max.Metadata = make(map[string]string, 64)
	for i := 0; i < 64; i++ {
		max.Metadata[fmt.Sprintf("%03d%s", i, strings.Repeat("k", 253))] = strings.Repeat("值", 1365) + "x"
	}
	out["maximum_fields"] = storedDecoderJSON(t, max)
	return out
}

func TestStoredSampleDecoderCompatibility(t *testing.T) {
	for name, data := range storedDecoderCorpus(t) {
		for _, seeded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/seeded_%t", name, seeded), func(t *testing.T) {
				var want, got health.Sample
				if seeded {
					want, got = storedDecoderSample(3), storedDecoderSample(3)
				}
				stdErr, fastErr := stdjson.Unmarshal(data, &want), gojson.Unmarshal(data, &got)
				if (stdErr == nil) != (fastErr == nil) {
					t.Fatalf("acceptance differs: standard=%v optimized=%v", stdErr, fastErr)
				}
				// The stored-row handler discards partial results on every error.
				if stdErr != nil {
					return
				}
				wantJSON, gotJSON := storedDecoderJSON(t, want), storedDecoderJSON(t, got)
				if !reflect.DeepEqual(want, got) || !bytes.Equal(wantJSON, gotJSON) {
					t.Fatalf("decoded fields differ: standard=%s optimized=%s", wantJSON, gotJSON)
				}
				if want.Value != nil && math.Float64bits(*want.Value) != math.Float64bits(*got.Value) {
					t.Fatal("decoded float bits differ")
				}
			})
		}
	}
}

func TestStoredSampleDecoderOwnsInputStrings(t *testing.T) {
	data := storedDecoderJSON(t, storedDecoderSample(3))
	var s health.Sample
	if err := gojson.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	before := storedDecoderJSON(t, s)
	// Database drivers may reuse scan buffers for the following row. This checks
	// every decoded string, including map keys and values, after input reuse.
	for i := range data {
		data[i] = 'x'
	}
	if !bytes.Equal(before, storedDecoderJSON(t, s)) {
		t.Fatal("decoded sample aliases the reusable database buffer")
	}
}
