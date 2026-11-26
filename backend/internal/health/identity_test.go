package health

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

func referenceIdentity(o Owner, s Sample) string {
	b, _ := json.Marshal([]any{o.OldID, o.DeviceID, o.Epoch, s.Source, s.SourceDevice, s.RecordID, s.Metric})
	return Digest(b)
}
func TestIdentityDigestEquivalent(t *testing.T) {
	random := rand.New(rand.NewSource(19471))
	sample := Sample{Source: "watch", SourceDevice: "device", RecordID: "record", Metric: "heart_rate"}
	device := int64(123)
	edge := []string{"", "simple", "quotes\"", "back\\slash", "escape\t\n\r", "<html>&", "中文手表", "\u2028", "\u2029", string([]byte{0xff, 0xfe}), "🙂", strings.Repeat("a", 5000)}
	cases := 0
	for _, e := range edge {
		for i := 0; i < 4; i++ {
			o := Owner{OldID: int64(i - 3), Epoch: int64(i)}
			if i%2 == 0 {
				o.DeviceID = &device
			}
			sample.Source = e
			if i%2 == 1 {
				sample.SourceDevice = e
			}
			if i%3 == 0 {
				sample.RecordID = e
			}
			got, want := identityDigest(o, sample), referenceIdentity(o, sample)
			if got != want {
				t.Fatalf("edge %q: got %s want %s", e, got, want)
			}
			cases++
		}
	}
	alphabet := []byte("abcABC019_@-:/ <>\\\"\n\t&")
	for i := 0; i < 5000; i++ {
		s := make([]byte, random.Intn(90))
		for j := range s {
			s[j] = alphabet[random.Intn(len(alphabet))]
		}
		sample.Source = string(s)
		sample.SourceDevice = string(s)
		sample.RecordID = string(s)
		o := Owner{OldID: random.Int63(), Epoch: int64(random.Intn(1000))}
		if i%2 == 0 {
			o.DeviceID = &device
		}
		got, want := identityDigest(o, sample), referenceIdentity(o, sample)
		if got != want {
			t.Fatalf("random case %d mismatch", i)
		}
		cases++
	}
	if cases < 5000 {
		t.Fatal("insufficient cases")
	}
}

var identitySink string

func BenchmarkIdentityLegacy(b *testing.B) {
	o := Owner{OldID: 456, Epoch: 1}
	dev := int64(124)
	o.DeviceID = &dev
	s := Sample{Source: "watch", SourceDevice: "WATCH-123456", RecordID: "sample-123456", Metric: "heart_rate"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		identitySink = referenceIdentity(o, s)
	}
}
func BenchmarkIdentityOptimized(b *testing.B) {
	o := Owner{OldID: 456, Epoch: 1}
	dev := int64(124)
	o.DeviceID = &dev
	s := Sample{Source: "watch", SourceDevice: "WATCH-123456", RecordID: "sample-123456", Metric: "heart_rate"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		identitySink = identityDigest(o, s)
	}
}
func BenchmarkIdentityOptimizedUnicode(b *testing.B) {
	o := Owner{OldID: 456, Epoch: 1}
	dev := int64(124)
	o.DeviceID = &dev
	s := Sample{Source: "watch", SourceDevice: "老人手表-A1", RecordID: "记录\u2028ONE", Metric: "heart_rate"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		identitySink = identityDigest(o, s)
	}
}
