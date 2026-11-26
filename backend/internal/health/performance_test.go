package health

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

var preparedBytes []byte

func sampleBenchmarkInput() (Owner, Sample) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	device := int64(123)
	return Owner{OldID: 456, DeviceID: &device, Epoch: 1}, Sample{RecordID: "sample-123456", Source: "watch", SourceDevice: "WATCH-123456", Metric: "heart_rate", Unit: "count/min", Value: Number(72), Start: at, End: at, Version: 1}
}

func BenchmarkSamplePreparation(b *testing.B) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	s := Sample{RecordID: "sample-123456", Source: "watch", SourceDevice: "WATCH-123456", Metric: "heart_rate", Unit: "count/min", Value: Number(72), Start: at, End: at, Version: 1}
	device := int64(123)
	o := Owner{OldID: 456, DeviceID: &device, Epoch: 1}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n, err := s.Normalize()
		if err != nil {
			b.Fatal(err)
		}
		payload, err := json.Marshal(n)
		if err != nil {
			b.Fatal(err)
		}
		identity, _ := json.Marshal([]any{o.OldID, o.DeviceID, o.Epoch, n.Source, n.SourceDevice, n.RecordID, n.Metric})
		id := Digest(identity)
		if len(changePayload(o, id, 987654321, payload)) == 0 {
			b.Fatal("empty change")
		}
	}
}

func TestChangePayloadMatchesRecordJSON(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 123456000, time.FixedZone("reporting", 8*60*60))
	s := Sample{RecordID: "quoted\"record", Source: "<watch>&", SourceDevice: "设备\\\n", Metric: "heart_rate", Unit: "count/min", Value: Number(72), Start: at, End: at, Metadata: map[string]string{"second": "\u2028", "first": "quoted\"value"}, Version: 2}
	id := Digest([]byte("identity"))
	device := int64(123)
	for _, owner := range []Owner{{OldID: 456, Epoch: 9}, {OldID: 456, DeviceID: &device, Epoch: 9}} {
		for _, deleted := range []bool{false, true} {
			s.Deleted = deleted
			s.Value = Number(72)
			if deleted {
				s.Value = nil
			}
			payload, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(Record{id, owner.OldID, owner.DeviceID, owner.Epoch, 987654321, s})
			if err != nil {
				t.Fatal(err)
			}
			got := changePayload(owner, id, 987654321, payload)
			if !bytes.Equal(got, want) {
				t.Fatalf("change JSON differs:\ngot  %s\nwant %s", got, want)
			}
		}
	}
}

func TestBuildValueRows(t *testing.T) {
	for _, tc := range []struct {
		rows, columns int
		want          string
	}{
		{1, 1, "(?)"}, {1, 3, "(?,?,?)"}, {2, 2, "(?,?),(?,?)"},
		{2, 14, "(?,?,?,?,?,?,?,?,?,?,?,?,?,?),(?,?,?,?,?,?,?,?,?,?,?,?,?,?)"},
	} {
		if got := buildValueRows(tc.rows, tc.columns); got != tc.want {
			t.Fatalf("placeholders = %q, want %q", got, tc.want)
		}
	}
	got := buildValueRows(200, 14)
	if strings.Count(got, "(") != 200 || !strings.HasSuffix(got, "?)") {
		t.Fatal("wrong bulk statement")
	}
	if got := buildValueRows(1, 200); strings.Count(got, "?") != 200 || !strings.HasSuffix(got, "?)") {
		t.Fatal("wrong sample lookup placeholders")
	}
}

// The sample was already JSON-encoded before taking the revision lock. Reuse
// those bytes in its immutable change event rather than encode it a second time.
// id is the hex SHA-256 identity produced by Digest, so it needs no escaping.
func changePayload(o Owner, id string, revision int64, sample []byte) []byte {
	b := make([]byte, 0, len(sample)+len(id)+128)
	b = append(b, `{"sampleId":"`...)
	b = append(b, id...)
	b = append(b, `","oldId":`...)
	b = strconv.AppendInt(b, o.OldID, 10)
	b = append(b, `,"deviceId":`...)
	if o.DeviceID == nil {
		b = append(b, "null"...)
	} else {
		b = strconv.AppendInt(b, *o.DeviceID, 10)
	}
	b = append(b, `,"bindingVersion":`...)
	b = strconv.AppendInt(b, o.Epoch, 10)
	b = append(b, `,"revision":`...)
	b = strconv.AppendInt(b, revision, 10)
	b = append(b, `,"sample":`...)
	b = append(b, sample...)
	return append(b, '}')
}
