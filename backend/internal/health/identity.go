package health

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"yihe-care/internal/jsonscan"
)

// identityDigest hashes exactly the JSON array used by the previous
// json.Marshal([]any{owner fields..., source fields...}) implementation. Normal
// device IDs contain ASCII, so appending into a stack buffer avoids interface
// boxes, reflection, and the intermediate JSON allocation. JSON encoding is
// still used for Unicode and escaped strings, preserving IDs byte-for-byte.
func identityDigest(o Owner, s Sample) string {
	var stack [512]byte
	b := stack[:0]
	b = append(b, '[')
	b = strconv.AppendInt(b, o.OldID, 10)
	b = append(b, ',')
	if o.DeviceID == nil {
		b = append(b, "null"...)
	} else {
		b = strconv.AppendInt(b, *o.DeviceID, 10)
	}
	b = append(b, ',')
	b = strconv.AppendInt(b, o.Epoch, 10)
	b = appendJSONStringIdentity(b, s.Source)
	b = appendJSONStringIdentity(b, s.SourceDevice)
	b = appendJSONStringIdentity(b, s.RecordID)
	b = appendJSONStringIdentity(b, s.Metric)
	b = append(b, ']')
	hash := sha256.Sum256(b)
	var encoded [64]byte
	hex.Encode(encoded[:], hash[:])
	return string(encoded[:])
}

// Keep the owner and consecutive equal source fields encoded in a request-local
// buffer. Only record ID and metric must be appended again for every sample.
func assignBatchIdentities(o Owner, input []preparedSample) {
	if len(input) == 1 {
		input[0].id, input[0].revision = identityDigest(o, input[0].sample), 0
		return
	}
	var stack [512]byte
	var encoded [64]byte
	b := append(stack[:0], '[')
	b = strconv.AppendInt(b, o.OldID, 10)
	b = append(b, ',')
	if o.DeviceID == nil {
		b = append(b, "null"...)
	} else {
		b = strconv.AppendInt(b, *o.DeviceID, 10)
	}
	b = append(b, ',')
	b = strconv.AppendInt(b, o.Epoch, 10)
	ownerEnd := len(b)
	prefixEnd := ownerEnd
	var source, device string
	for i := range input {
		s := &input[i].sample
		if i == 0 || source != s.Source || device != s.SourceDevice {
			b = appendJSONStringIdentity(b[:ownerEnd], s.Source)
			b = appendJSONStringIdentity(b, s.SourceDevice)
			prefixEnd = len(b)
			source, device = s.Source, s.SourceDevice
		}
		b = appendJSONStringIdentity(b[:prefixEnd], s.RecordID)
		b = appendJSONStringIdentity(b, s.Metric)
		b = append(b, ']')
		hash := sha256.Sum256(b)
		hex.Encode(encoded[:], hash[:])
		input[i].id, input[i].revision = string(encoded[:]), 0
	}
}

func appendJSONStringIdentity(b []byte, value string) []byte {
	b = append(b, ',', '"')
	if jsonscan.Safe(value) {
		return append(append(b, value...), '"')
	}
	b = b[:len(b)-1]
	quoted, _ := json.Marshal(value)
	return append(b, quoted...)
}
