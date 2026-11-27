package health

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestHistoryReferenceSingleCipher(t *testing.T) {
	p, box, samples := protectedWriteFixture(t)
	ctx := t.Context()
	a, b := samples[0], samples[1]
	if _, err := saveProtectedFixture(ctx, p, box, []Sample{a, b}); err != nil {
		t.Fatal(err)
	}
	var count, size int
	if err := p.QueryRowContext(ctx, "SELECT COUNT(*),SUM(OCTET_LENGTH(payload_cipher)) FROM health_sample WHERE payload_cipher=0x01").Scan(&count, &size); err != nil || count != 2 || size != 2 {
		t.Fatal("physical references", count, size, err)
	}
	id := identityDigest(Owner{OldID: 1}, a)
	var cipher string
	var revision int64
	if err := p.QueryRowContext(ctx, "SELECT revision,"+SampleCipherSQL+" FROM health_sample WHERE sample_id=UNHEX(?)", id).Scan(&revision, &cipher); err != nil {
		t.Fatal(err)
	}
	sample, err := (ProjectionPayload{Cipher: &cipher}).OpenSample(box, id, 1, revision)
	if err != nil {
		t.Fatal(err)
	}
	page, err := ListChanges(ctx, p, 1, 0, 10, box)
	if err != nil || len(page.Items) != 2 {
		t.Fatal(page, err)
	}
	var event Record
	if err = json.Unmarshal(page.Items[0], &event); err != nil || event.SampleID != id {
		t.Fatal(event, err)
	}
	want, err := json.Marshal(event.Sample)
	if err != nil || !bytes.Equal(sample, want) {
		t.Fatal("sample differs from history", err)
	}
	// Existing independently sealed rows still authenticate and transition
	// to references on a genuine revision, not on duplicate delivery.
	legacy, err := box.SealBytes(sample, samplePayloadScope(id, 1, revision))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "UPDATE health_sample SET payload_cipher=? WHERE sample_id=UNHEX(?)", legacy, id); err != nil {
		t.Fatal(err)
	}
	if d, err := saveProtectedFixture(ctx, p, box, []Sample{a, b}); err != nil || d != 2 {
		t.Fatal("legacy and reference replay", d, err)
	}
	a.Version++
	if _, err = saveProtectedFixture(ctx, p, box, []Sample{a}); err != nil {
		t.Fatal(err)
	}
	if err = VerifySampleReferenceIntegrity(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRowContext(ctx, "SELECT revision FROM health_sample WHERE sample_id=UNHEX(?) AND payload_cipher=0x01", id).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ExecContext(ctx, "DELETE FROM health_change WHERE revision=?", revision); err != nil {
		t.Fatal(err)
	}
	if err = VerifySampleReferenceIntegrity(ctx, p); err == nil {
		t.Fatal("missing history accepted")
	}
	a.Version++
	if _, err = saveProtectedFixture(ctx, p, box, []Sample{a}); err == nil {
		t.Fatal("missing obsolete history accepted")
	}
}
