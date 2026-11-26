package handlers

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// Fixed list avoids constructing a new map on every high-frequency device report.
var telemetryTextLimits = [...]struct {
	field string
	limit int
}{
	{"hr", 12}, {"bp", 12}, {"step", 12}, {"sleep", 12},
	{"sleepState", 1}, {"lng", 20}, {"lat", 20},
	{"source", 20}, {"beTime", 19}, {"battery", 3},
	{"appVersion", 20},
}

func checkTelemetry(w http.ResponseWriter, m map[string]any) bool {
	for _, field := range telemetryTextLimits {
		if s, present := vstr(m, field.field); present && s != "" && utf8.RuneCountInString(s) > field.limit {
			writeValidation400(w)
			return false
		}
	}
	// alarm 允许 "1" 或 "fall"（规格明确），其余按 ≤1 字符校验。
	if s, present := vstr(m, "alarm"); present && s != "" &&
		!strings.EqualFold(s, "fall") && utf8.RuneCountInString(s) > 1 {
		writeValidation400(w)
		return false
	}
	return true
}
