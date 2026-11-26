package health

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestMarshalSampleFastByteExact(t *testing.T) {
	rand1 := rand.New(rand.NewSource(310581))
	_, original := sampleBenchmarkInput()
	oddStrings := []string{"watch", "设备-汉字", "\tbad", "a\\bc", "<bad>&", "emoji🙂", string([]byte{0xff, 0xfe}), ""}
	for i := 0; i < 10000; i++ {
		s := original
		switch i % 8 {
		case 0:
			s.SourceDevice = oddStrings[i%len(oddStrings)]
			s.RecordID = oddStrings[(i+1)%len(oddStrings)]
		case 1:
			s.SourceVersion = "1.1"
			s.GroupID = "grp"
			s.Stage = "deep"
			s.TimeBasis = "legacy-unzoned"
			s.Metadata = map[string]string{"foo": "bar"}
		case 2:
			s.Deleted = true
			s.Value = nil
		case 3:
			s.Source = oddStrings[i%len(oddStrings)]
		case 4:
			s.Start = time.Date(2026, 10, 20, 12, 0, 0, rand1.Intn(1000000000), time.FixedZone("odd", 30))
		case 5:
			s.Start = time.Date(2026, 10, 20, 12, 0, 0, rand1.Intn(1000000000), time.UTC)
		}
		val := []float64{math.Copysign(0, -1), 1e-7, 1e21, 1e26, 1.3, 1234, -345, math.MaxFloat64, float64(rand1.Intn(101))}[i%9]
		if s.Value != nil {
			s.Value = &val
		}
		canonical, err1 := json.Marshal(s)
		fast, err2 := marshalSampleFast(s)
		if (err1 == nil) != (err2 == nil) || (err1 == nil && !bytes.Equal(canonical, fast)) {
			t.Fatalf("case %d canonical=%s err=%v; fast=%s err=%v", i, canonical, err1, fast, err2)
		}
	}
}
func BenchmarkSamplePrepFast(b *testing.B) {
	o, s := sampleBenchmarkInput()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n, err := s.Normalize()
		if err != nil {
			b.Fatal(err)
		}
		data, err := marshalSampleFast(n)
		if err != nil {
			b.Fatal(err)
		}
		identitySink = identityDigest(o, n)
		preparedBytes = data
	}
}
func BenchmarkSamplePrepFastUnicode(b *testing.B) {
	o, s := sampleBenchmarkInput()
	s.SourceDevice = "北区老人手表"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n, err := s.Normalize()
		if err != nil {
			b.Fatal(err)
		}
		data, err := marshalSampleFast(n)
		if err != nil {
			b.Fatal(err)
		}
		identitySink = identityDigest(o, n)
		preparedBytes = data
	}
}
