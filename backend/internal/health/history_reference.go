package health

import (
	"context"
	"encoding/json"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

// This binary reference is disjoint from all securestore envelopes. It denotes
// an immutable revision in health_change, not plaintext or a key identifier.
// The existing protection constraint and routing index remain intact.
// Drain old readers and writers before enabling this representation.
// Keep referenced change events immutable and retained. A downgrade requires
// restoring a matching backup or materializing every reference before use.
const sampleHistoryReference = "\x01"

const sampleResolvedTail = ".payload_cipher=0x01 THEN CONCAT(0x01,COALESCE((SELECT hc.payload_cipher FROM health_change hc WHERE hc.revision="
const SampleCipherSQL = "CASE WHEN health_sample" + sampleResolvedTail + "health_sample.revision AND hc.old_id=health_sample.old_id AND hc.sample_id=health_sample.sample_id),_binary'')) ELSE health_sample.payload_cipher END"
const AliasedSampleCipherSQL = "CASE WHEN s" + sampleResolvedTail + "s.revision AND hc.old_id=s.old_id AND hc.sample_id=s.sample_id),_binary'')) ELSE s.payload_cipher END"

// The reference is never considered an authenticated value. Missing,
// malformed and substituted history records fail closed.
func openHistorySample(p ProjectionPayload, box *securestore.Box, id string, owner, revision int64, scratch *[]byte) (json.RawMessage, error) {
	if p.JSON != nil || p.Cipher == nil || len(*p.Cipher) <= len(sampleHistoryReference) {
		return nil, errors.New("missing referenced health history payload")
	}
	cipher := (*p.Cipher)[len(sampleHistoryReference):]
	payload, err := (ProjectionPayload{Cipher: &cipher}).open(box, changePayloadScope(revision, owner, id), scratch)
	if err != nil {
		return nil, err
	}
	var record struct {
		SampleID string
		OldID    int64
		Revision int64
		Sample   json.RawMessage
	}
	if err = json.Unmarshal(payload, &record); err != nil || record.SampleID != id || record.OldID != owner || record.Revision != revision || len(record.Sample) == 0 || record.Sample[0] != '{' {
		return nil, errors.New("referenced health history identity mismatch")
	}
	return record.Sample, nil
}

// VerifySampleReferenceIntegrity rejects a missing or mismatched historical
// reference before maintenance. The history rotation scan independently
// authenticates every referenced envelope.
func VerifySampleReferenceIntegrity(ctx context.Context, p db.RowQuerier) error {
	var broken bool
	err := p.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM health_sample s LEFT JOIN health_change c ON c.revision=s.revision AND c.old_id=s.old_id AND c.sample_id=s.sample_id WHERE s.payload_cipher=0x01 AND (c.payload_cipher IS NULL OR c.payload IS NOT NULL))").Scan(&broken)
	if err != nil {
		return err
	}
	if broken {
		return errors.New("unresolved health sample history reference")
	}
	return nil
}
