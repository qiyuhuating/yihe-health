package health

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"
)

func BenchmarkStatisticsProjectionChanges(b *testing.B) {
	from := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	samples := make([]Sample, 128)
	for i := range samples {
		at := from.Add(time.Duration(i) * time.Second)
		samples[i] = Sample{RecordID: fmt.Sprint(i), Source: "watch", SourceDevice: "WATCH001", Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Start: at, End: at, Version: 1, Value: Number(72), Metadata: map[string]string{"sensor": "heart_rate", "timestampBasis": "utc"}}
	}
	batch, err := PrepareBatch(samples)
	if err != nil {
		b.Fatal(err)
	}
	owner := Owner{OldID: 1, Epoch: 1}
	assignBatchIdentities(owner, batch.input)
	for i := range batch.input {
		batch.input[i].revision = int64(i + 1)
	}
	batch.projectionThrough = int64(len(samples))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		changes, err := batch.StatisticsProjectionChanges(owner)
		if err != nil || len(changes) != 128 || *changes[127].Sample.Value != 72 {
			b.Fatal("projection")
		}
	}
}

func TestStatisticsProjectionChangesCanonicalFields(t *testing.T) {
	at := time.Date(2026, 10, 22, 0, 0, 0, 123456789, time.FixedZone("seconds", 61))
	samples := []Sample{
		{RecordID: "a", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Start: at.UTC(), End: at.UTC(), Version: 1, Value: Number(math.Copysign(0, -1)), Metadata: map[string]string{"sensor": "hr"}},
		{RecordID: "b", Source: string([]byte{0xff}), SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Start: at, End: at, Version: 1, Value: Number(80)},
		{RecordID: "c", Source: "watch", SourceDevice: "A", Metric: "heart_rate", Unit: "count/min", Semantics: "instant", Start: at.UTC(), End: at.UTC(), Version: 2, Deleted: true},
	}
	batch, err := PrepareBatch(samples)
	if err != nil {
		t.Fatal(err)
	}
	owner := Owner{OldID: 1, Epoch: 1}
	assignBatchIdentities(owner, batch.input)
	for i := range batch.input {
		batch.input[i].revision = int64(i + 1)
	}
	batch.projectionThrough = 3
	*samples[0].Value = 99
	samples[0].Metadata["sensor"] = "changed"
	got, err := batch.StatisticsProjectionChanges(owner)
	if err != nil || len(got) != 3 {
		t.Fatalf("projection %v %v", got, err)
	}
	for i, record := range got {
		var want Sample
		if err := json.Unmarshal(batch.input[i].payload, &want); err != nil {
			t.Fatal(err)
		}
		s := record.Sample
		if record.Revision != int64(i+1) || s.Source != want.Source || s.SourceDevice != want.SourceDevice ||
			s.Metric != want.Metric || s.Unit != want.Unit || s.Semantics != want.Semantics || s.TimeBasis != want.TimeBasis ||
			!s.Start.Equal(want.Start) || s.Deleted != want.Deleted || (s.Value == nil) != (want.Value == nil) ||
			(s.Value != nil && math.Float64bits(*s.Value) != math.Float64bits(*want.Value)) {
			t.Fatalf("projection fields differ for %d: %+v vs %+v", i, s, want)
		}
	}
}
