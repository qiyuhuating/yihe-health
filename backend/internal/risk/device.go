package risk

import (
	"regexp"
	"strings"
)

// DeviceInfo is a best-effort browser hint, never a verified device identity.
// User-Agent and Client Hints are client-controlled. Do not authorize or ban by model.
type DeviceInfo struct {
	Type           string `json:"deviceType"`
	OS             string `json:"os"`
	Browser        string `json:"browser"`
	Model          string `json:"model,omitempty"`
	ModelSource    string `json:"modelSource,omitempty"`
	UserAgent      string `json:"userAgent,omitempty"`
	HardwareID     string `json:"hardwareId,omitempty"`
	HardwareSource string `json:"hardwareSource,omitempty"`
}

var androidModel = regexp.MustCompile(`(?i)android[^;)]*;\s*([^;)]+?)(?:\s+build/[^;)]*)?\s*[;)]`)

func cleanModel(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	if len(s) < 2 || len(s) > 80 {
		return ""
	}
	low := strings.ToLower(s)
	if low == "wv" || low == "u" || low == "mobile" || low == "linux" || low == "k" || strings.Contains(low, "android") {
		return ""
	}
	for _, r := range s {
		if r < 32 || r > 126 && r < 160 {
			return ""
		}
	}
	return s
}

// IdentifyDevice accepts User-Agent and optional Sec-CH-UA-Model; both are untrusted.
// It bounds persisted data and does not assume mobile browsers expose exact hardware models.
func IdentifyDevice(signals ...string) DeviceInfo {
	var ua, hint string
	var hardwareID string
	if len(signals) > 0 {
		ua = strings.TrimSpace(signals[0])
	}
	if len(signals) > 1 {
		hint = strings.TrimSpace(signals[1])
	}
	if len(signals) > 2 {
		hardwareID = strings.TrimSpace(signals[2])
	}
	if len(ua) > 512 {
		ua = ua[:512]
	}
	d := DeviceInfo{Type: "unknown", OS: "Unknown", Browser: "Unknown", UserAgent: ua}
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "android"):
		d.OS, d.Type = "Android", "mobile"
		if strings.Contains(l, "tablet") || (!strings.Contains(l, "mobile") && strings.Contains(l, "android")) {
			d.Type = "tablet"
		}
	case strings.Contains(l, "iphone"):
		d.OS, d.Type = "iOS", "mobile"
	case strings.Contains(l, "ipad"):
		d.OS, d.Type = "iPadOS", "tablet"
	case strings.Contains(l, "windows"):
		d.OS, d.Type = "Windows", "desktop"
	case strings.Contains(l, "macintosh") || strings.Contains(l, "mac os x"):
		d.OS, d.Type = "macOS", "desktop"
	case strings.Contains(l, "linux"):
		d.OS, d.Type = "Linux", "desktop"
	}
	switch {
	case strings.Contains(l, "edg/") || strings.Contains(l, "edga/") || strings.Contains(l, "edgios/"):
		d.Browser = "Edge"
	case strings.Contains(l, "opr/") || strings.Contains(l, "opera"):
		d.Browser = "Opera"
	case strings.Contains(l, "firefox/") || strings.Contains(l, "fxios/"):
		d.Browser = "Firefox"
	case strings.Contains(l, "chrome/") || strings.Contains(l, "crios/"):
		d.Browser = "Chrome"
	case strings.Contains(l, "safari/"):
		d.Browser = "Safari"
	}
	if d.OS == "Android" {
		if m := androidModel.FindStringSubmatch(ua); len(m) > 1 {
			if model := cleanModel(m[1]); model != "" {
				d.Model, d.ModelSource = model, "user-agent"
			}
		}
		// Client hints are a secondary reported signal, not attested hardware.
		if d.Model == "" {
			if model := cleanModel(hint); model != "" {
				d.Model, d.ModelSource = model, "client-hint"
			}
		}
	}
	// Native clients may submit an app-scoped identifier (not IMEI/MAC).
	// This is reported, not hardware-attested, so it must not authorize access.
	if len(hardwareID) >= 16 && len(hardwareID) <= 128 {
		valid := true
		for _, c := range hardwareID {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
				valid = false
				break
			}
		}
		if valid {
			d.HardwareID, d.HardwareSource = hardwareID, "app-reported"
		}
	}
	return d
}
