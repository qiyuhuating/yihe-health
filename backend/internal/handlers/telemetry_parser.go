package handlers

import (
	"net/http"
	"strings"
	"time"
)

// parsedDeviceTelemetry holds values already checked before opening a database
// transaction. The existing watch wire format and response schema are retained.
type parsedDeviceTelemetry struct {
	now, beTime, day                      time.Time
	hr, bp, step, sleep, battery          *int
	sleepState, appVersion                *string
	lng, lat, sourceStr, alarm, requestID string
	position                              positionMetadata
}

func parseDeviceTelemetryPayload(w http.ResponseWriter, m map[string]any, now time.Time) (parsedDeviceTelemetry, bool) {
	out := parsedDeviceTelemetry{now: now, sourceStr: "gps"}
	beTimeStr, _ := vstr(m, "beTime")
	if beTimeStr == "" {
		beTimeStr = now.Format(tsLayout)
	}
	var valid bool
	out.beTime, valid = parseReportTimeStrict(beTimeStr)
	if !valid {
		write400(w, "beTime 格式应为 yyyy-MM-dd HH:mm:ss")
		return parsedDeviceTelemetry{}, false
	}
	// Legacy timestamps use server wall time without a zone. Compare in the
	// same representation so a non-UTC server does not reject its own default.
	wallNow, _ := parseReportTimeStrict(now.Format(tsLayout))
	if out.beTime.After(wallNow.Add(5 * time.Minute)) {
		write400(w, "beTime 不能晚于服务器时间 5 分钟，请检查设备时钟")
		return parsedDeviceTelemetry{}, false
	}
	out.day, _ = time.Parse("2006-01-02", beTimeStr[:10])

	var msg string
	if out.hr, msg = parseMetric(m, "hr", 20, 250); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if out.bp, msg = parseMetric(m, "bp", 40, 300); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if out.step, msg = parseMetric(m, "step", 0, 200000); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if out.sleep, msg = parseMetric(m, "sleep", 0, 24); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if out.sleepState, msg = parseText(m, "sleepState", 1); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if out.sleepState != nil && *out.sleepState != "0" && *out.sleepState != "1" {
		write400(w, "sleepState 应为 0 或 1")
		return parsedDeviceTelemetry{}, false
	}
	out.lng, _ = vstr(m, "lng")
	out.lat, _ = vstr(m, "lat")
	if msg := checkPosition(out.lng, out.lat); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	var positionErr error
	out.position, positionErr = parsePositionMetadata(m, out.lng, out.lat, now)
	if positionErr != nil {
		write400(w, positionErr.Error())
		return parsedDeviceTelemetry{}, false
	}
	source, msg := parseText(m, "source", 20)
	if msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if source != nil {
		out.sourceStr = *source
	}
	if out.battery, msg = parseMetric(m, "battery", 0, 100); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	if out.appVersion, msg = parseText(m, "appVersion", 20); msg != "" {
		write400(w, msg)
		return parsedDeviceTelemetry{}, false
	}
	out.alarm, _ = vstr(m, "alarm")
	// An unknown emergency marker must not quietly become a normal report.
	// The SOS button has its own endpoint and is never accepted as telemetry.
	if out.alarm != "" && out.alarm != "0" && out.alarm != "1" && !strings.EqualFold(out.alarm, "fall") {
		write400(w, "alarm 仅支持 0、1 或 fall")
		return parsedDeviceTelemetry{}, false
	}
	return out, true
}
