package telemetryworkflow

import (
	"encoding/json"
	"errors"
	"time"

	"yihe-care/internal/securestore"
)

// Payload is the durable encrypted wire format. It is intentionally restricted
// to non-emergency, non-vital measurements.
type Payload struct {
	ReceivedAt time.Time `json:"receivedAt"`
	ReportedAt time.Time `json:"reportedAt"`
	Step       *int      `json:"step,omitempty"`
	Sleep      *int      `json:"sleep,omitempty"`
	SleepState *string   `json:"sleepState,omitempty"`
	Battery    *int      `json:"battery,omitempty"`
	AppVersion *string   `json:"appVersion,omitempty"`
}

func Encode(box *securestore.Box, payload Payload) (string, error) {
	if box == nil {
		return "", errors.New("durable telemetry requires a valid encryption key")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return box.Seal(string(data), "telemetry-inbox")
}

func Decode(box *securestore.Box, sealed string) (Payload, error) {
	if box == nil {
		return Payload{}, errors.New("missing telemetry encryption key")
	}
	plain, err := box.Open(sealed, "telemetry-inbox")
	if err != nil {
		return Payload{}, err
	}
	var payload Payload
	if err := json.Unmarshal([]byte(plain), &payload); err != nil {
		return Payload{}, err
	}
	if payload.ReportedAt.IsZero() || payload.ReceivedAt.IsZero() {
		return Payload{}, errors.New("missing report timestamp")
	}
	return payload, nil
}
