package handlers

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"yihe-care/internal/securestore"
)

// Reported is a device claim with valid syntax, not independently verified positioning.
type positionMetadata struct {
	Version   int        `json:"version"`
	Source    *string    `json:"source,omitempty"`
	SampledAt *time.Time `json:"sampledAt,omitempty"`
	Accuracy  *float64   `json:"accuracy,omitempty"`
	Validity  string     `json:"validity"`
}

func parsePositionMetadata(m map[string]any, lng, lat string, now time.Time) (positionMetadata, error) {
	meta := positionMetadata{Version: 1, Validity: "missing"}
	supplied := false
	if raw := m["positionSource"]; raw != nil {
		source, ok := raw.(string)
		if !ok {
			return meta, errors.New("positionSource 应为明确的定位来源")
		}
		switch source {
		case "gps", "network", "fused", "manual", "simulated", "unknown":
		default:
			return meta, errors.New("positionSource 应为 gps、network、fused、manual、simulated 或 unknown")
		}
		meta.Source, supplied = &source, true
	}
	if raw := m["positionSampledAt"]; raw != nil {
		value, ok := raw.(string)
		if !ok {
			return meta, errors.New("positionSampledAt 应为带时区的 RFC3339 时间")
		}
		sampled, err := time.Parse(time.RFC3339Nano, value)
		if err != nil || sampled.Year() < 1 || sampled.After(now.Add(5*time.Minute)) {
			return meta, errors.New("positionSampledAt 无效或晚于服务器时间五分钟")
		}
		sampled = sampled.UTC()
		meta.SampledAt, supplied = &sampled, true
	}
	if raw := m["positionAccuracy"]; raw != nil {
		accuracy, ok := raw.(float64)
		if !ok || math.IsNaN(accuracy) || math.IsInf(accuracy, 0) || accuracy <= 0 {
			return meta, errors.New("positionAccuracy 应为以米为单位的有限正数")
		}
		meta.Accuracy, supplied = &accuracy, true
	}
	if lng == "" || lat == "" {
		if supplied {
			return meta, errors.New("定位元数据须与本次完整经纬度一起提供")
		}
		return meta, nil
	}
	meta.Validity = "reported"
	return meta, nil
}

func sealPositionMetadata(box *securestore.Box, meta positionMetadata) (string, error) {
	data, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	if box == nil {
		return string(data), nil
	}
	return box.Seal(string(data), coordinateScope)
}

func openPositionMetadata(box *securestore.Box, raw *string) (positionMetadata, error) {
	meta := positionMetadata{Version: 1, Validity: "unknown"}
	if raw == nil {
		return meta, nil
	}
	data := *raw
	if strings.HasPrefix(data, securestore.Prefix) {
		if box == nil {
			return meta, errors.New("position metadata key unavailable")
		}
		var err error
		data, err = box.Open(data, coordinateScope)
		if err != nil {
			return meta, err
		}
	}
	if err := json.Unmarshal([]byte(data), &meta); err != nil {
		return meta, err
	}
	if meta.Version != 1 || (meta.Validity != "reported" && meta.Validity != "missing") {
		return meta, errors.New("invalid position metadata version or validity")
	}
	return meta, nil
}

func metadataSampleTime(at *time.Time) any {
	if at == nil {
		return nil
	}
	return at.UTC().Format(time.RFC3339Nano)
}
