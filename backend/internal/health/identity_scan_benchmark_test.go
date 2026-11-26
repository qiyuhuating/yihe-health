package health

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// identityDigest hashes exactly the JSON array used by the previous
// json.Marshal([]any{owner fields..., source fields...}) implementation. Normal
// device IDs contain ASCII, so appending into a stack buffer avoids interface
// boxes, reflection, and the intermediate JSON allocation. JSON encoding is
// still used for Unicode and escaped strings, preserving IDs byte-for-byte.
func identityDigestScalarReference(o Owner, s Sample) string {
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
	b = appendJSONStringScalarReference(b, s.Source)
	b = appendJSONStringScalarReference(b, s.SourceDevice)
	b = appendJSONStringScalarReference(b, s.RecordID)
	b = appendJSONStringScalarReference(b, s.Metric)
	b = append(b, ']')
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

func appendJSONStringScalarReference(b []byte, value string) []byte {
	b = append(b, ',', '"')
	safe := true
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < 0x20 || c >= 0x80 || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			safe = false
			break
		}
	}
	if safe {
		return append(append(b, value...), '"')
	}
	b = b[:len(b)-1]
	quoted, _ := json.Marshal(value)
	return append(b, quoted...)
}

func BenchmarkIdentityScan(b *testing.B) {
	for _, n := range []int{8, 32, 64, 256} {
		o := Owner{OldID: 456, Epoch: 1}
		s := Sample{Source: "watch", SourceDevice: strings.Repeat("d", n), RecordID: strings.Repeat("r", n), Metric: "heart_rate"}
		b.Run(fmt.Sprintf("%d/Original", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				identitySink = identityDigestScalarReference(o, s)
			}
		})
		b.Run(fmt.Sprintf("%d/Selected", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				identitySink = identityDigest(o, s)
			}
		})
	}
}
