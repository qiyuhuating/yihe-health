package health

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"

	"yihe-care/internal/securestore"
)

const maxProjectionCipherBytes = 8 << 20

type ProjectionPayload struct {
	JSON   []byte
	Cipher *string
}

func ChangePayloadScope(revision, owner, id string) string {
	return "health-change:" + revision + ":" + owner + ":" + id
}

func SamplePayloadScope(id, owner, revision string) string {
	return "health-sample:" + id + ":" + owner + ":" + revision
}

func samplePayloadScope(id string, owner, revision int64) string {
	return SamplePayloadScope(id, strconv.FormatInt(owner, 10), strconv.FormatInt(revision, 10))
}

func changePayloadScope(revision, owner int64, id string) string {
	return ChangePayloadScope(strconv.FormatInt(revision, 10), strconv.FormatInt(owner, 10), id)
}

func LatestPayloadScope(owner, series, revision string) string {
	return "health-latest:" + owner + ":" + hex.EncodeToString([]byte(series)) + ":" + revision
}

func latestPayloadScope(owner int64, series []byte, revision int64) string {
	return LatestPayloadScope(strconv.FormatInt(owner, 10), string(series), strconv.FormatInt(revision, 10))
}

func DailyResultScope(id, revision string) string {
	return "health-daily:" + id + ":" + revision
}

func dailyResultScope(id string, revision int64) string {
	return DailyResultScope(id, strconv.FormatInt(revision, 10))
}

func protectProjection(box *securestore.Box, raw json.RawMessage, scope string) (ProjectionPayload, error) {
	if box == nil {
		return ProjectionPayload{JSON: raw}, nil
	}
	cipher, err := box.SealBytes(raw, scope)
	if err != nil || len(cipher) > maxProjectionCipherBytes {
		return ProjectionPayload{}, errors.New("cannot seal health projection payload")
	}
	return ProjectionPayload{Cipher: &cipher}, nil
}

func (p ProjectionPayload) Open(box *securestore.Box, scope string) (json.RawMessage, error) {
	if p.Cipher == nil {
		return p.JSON, nil
	}
	if p.JSON != nil || len(*p.Cipher) > maxProjectionCipherBytes {
		return nil, errors.New("invalid protected health projection payload")
	}
	plain, err := box.Open(*p.Cipher, scope)
	if err != nil || !json.Valid([]byte(plain)) {
		return nil, errors.New("health projection payload authentication failed")
	}
	return json.RawMessage(plain), nil
}

func (p ProjectionPayload) OpenDailyResult(box *securestore.Box, id string, revision int64) (json.RawMessage, error) {
	return p.Open(box, dailyResultScope(id, revision))
}

func (p ProjectionPayload) OpenSample(box *securestore.Box, id string, owner, revision int64) (json.RawMessage, error) {
	if p.JSON == nil && p.Cipher == nil {
		return nil, errors.New("health sample payload is missing")
	}
	return p.Open(box, samplePayloadScope(id, owner, revision))
}
