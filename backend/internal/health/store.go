package health

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"yihe-care/internal/securestore"
)

var ErrConflict = errors.New("same source record version has different content")
var ErrSourceDeviceMismatch = errors.New("sample source device does not match authenticated device")

type Owner struct {
	OldID    int64
	DeviceID *int64
	Epoch    int64
}
type Record struct {
	SampleID string `json:"sampleId"`
	OldID    int64  `json:"oldId"`
	DeviceID *int64 `json:"deviceId"`
	Epoch    int64  `json:"bindingVersion"`
	Revision int64  `json:"revision"`
	Sample   Sample `json:"sample"`
}

func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// Save participates in the caller's transaction; ownership is part of identity.
func Save(ctx context.Context, tx *sql.Tx, o Owner, s Sample) (int64, bool, error) {
	revision, duplicates, err := SaveBatch(ctx, tx, o, []Sample{s})
	return revision, duplicates == 1, err
}

// SaveBatch preserves the ordered version history of every input while writing
// final samples and their change events in two SQL statements. The revision
// owner clock remains locked through commit, preserving each owner's cursor
// ordering. MySQL reserves globally unique revision ranges infrequently.
func SaveBatch(ctx context.Context, tx *sql.Tx, o Owner, samples []Sample) (int64, int, error) {
	batch, err := PrepareBatch(samples)
	if err != nil {
		return 0, 0, err
	}
	return batch.Save(ctx, tx, o)
}

// PreparedBatch holds validated, encoded samples without an owner or revision.
// It belongs to one request and must not be saved concurrently.
type PreparedBatch struct {
	input          []preparedSample
	minimumVersion int64
}

// PrepareBatch performs owner-independent work before acquiring database locks.
func PrepareBatch(samples []Sample) (PreparedBatch, error) {
	return prepareBatch(samples, nil)
}

// PrepareDeviceBatch checks each source before normalizing that sample, retaining
// upload validation order without a second pass over the input.
func PrepareDeviceBatch(device string, samples []Sample) (PreparedBatch, error) {
	return prepareBatch(samples, &device)
}

func prepareBatch(samples []Sample, device *string) (PreparedBatch, error) {
	if len(samples) < 1 || len(samples) > 200 {
		return PreparedBatch{}, fmt.Errorf("batch must contain 1..200 samples")
	}
	input := make([]preparedSample, 0, len(samples))
	minimumVersion := samples[0].Version
	for _, s := range samples {
		if device != nil && s.SourceDevice != *device {
			return PreparedBatch{}, ErrSourceDeviceMismatch
		}
		s, err := s.Normalize()
		if err != nil {
			return PreparedBatch{}, err
		}
		minimumVersion = min(minimumVersion, s.Version)
		b, err := marshalSampleFast(s)
		if err != nil {
			return PreparedBatch{}, err
		}
		// Identity and SQL columns use immutable fields; the encoded snapshot
		// owns the value and metadata, so retain no caller-owned mutable data.
		s.Value, s.Metadata = nil, nil
		input = append(input, preparedSample{payload: b, sample: s})
	}
	return PreparedBatch{input: input, minimumVersion: minimumVersion}, nil
}

// Save derives identity from the caller's locked owner and preserves the same
// version/conflict/history transaction as SaveBatch.
func (batch *PreparedBatch) Save(ctx context.Context, tx *sql.Tx, o Owner) (int64, int, error) {
	return batch.SaveProtected(ctx, tx, o, nil)
}

// SaveProtected also seals samples, change events and latest-series payloads when
// the service data key is configured.
func (batch *PreparedBatch) SaveProtected(ctx context.Context, tx *sql.Tx, o Owner, box *securestore.Box) (int64, int, error) {
	if batch == nil || len(batch.input) < 1 || len(batch.input) > 200 {
		return 0, 0, fmt.Errorf("batch must contain 1..200 samples")
	}
	input := batch.input
	assignBatchIdentities(o, input)
	ids := make([]any, 1, len(input)+1)
	seen := make(map[string]uint8, len(input))
	for i := range input {
		id := input[i].id
		if _, exists := seen[id]; !exists {
			ids = append(ids, id)
			seen[id] = 0
		}
	}
	clock, err := lockOwner(ctx, tx, o.OldID)
	if err != nil {
		return 0, 0, err
	}
	previous, err := loadStoredSamples(ctx, tx, ids, batch.minimumVersion, o.OldID, box)
	if err != nil {
		return 0, 0, err
	}
	selection, err := selectBatch(input, previous, seen)
	if err != nil {
		return 0, 0, err
	}
	if len(selection.events) == 0 {
		return selection.cursor, selection.duplicates, nil
	}
	revision, err := clock.reserve(ctx, tx, o.OldID, len(selection.events))
	if err != nil {
		return 0, 0, err
	}
	for _, index := range selection.events {
		revision++
		input[index].revision = revision
	}
	cursor := max(selection.cursor, revision)
	scratch := borrowBatchArgs(max(len(selection.final)*15, len(selection.events)*5))
	defer releaseBatchArgs(scratch)
	args := scratch.values[:0]
	ownerID, epoch := any(o.OldID), any(o.Epoch)
	for _, index := range selection.final {
		item := &input[index]
		id := item.id
		s := &item.sample
		stored, err := protectProjection(box, item.payload, samplePayloadScope(id, o.OldID, item.revision))
		if err != nil {
			return 0, 0, err
		}
		args = append(args, id, ownerID, o.DeviceID, epoch, s.Version, item.revision, stored.JSON, stored.Cipher,
			s.Source, s.SourceDevice, s.Metric, s.Semantics, s.Start.UnixMicro(), s.End.UnixMicro(), s.Deleted)
	}
	_, err = tx.ExecContext(ctx, batchSQL(batchUpsert, len(selection.final)), args...)
	if err != nil {
		return 0, 0, err
	}
	// Exec consumes arguments synchronously; reuse the transaction-private backing
	// array for history instead of allocating another large pointer slice.
	args = args[:0]
	args, history, err := appendChangeRows(args, o, input, selection.events, box)
	defer releaseHistoryBuffer(history)
	if err != nil {
		return 0, 0, err
	}
	_, err = tx.ExecContext(ctx, batchSQL(batchChanges, len(selection.events)), args...)
	if err != nil {
		return 0, 0, fmt.Errorf("save health change: %w", err)
	}
	if err = refreshLatest(ctx, tx, o, input, selection.final, box); err != nil {
		return 0, 0, err
	}
	// Invalidate all cached days for this owner, including the OLD date of a
	// corrected/deleted interval. Skip already-dirty rows to avoid needless
	// update locks, redo work and cache invalidation churn.
	if _, err = tx.ExecContext(ctx, "UPDATE metric_daily SET dirty=true WHERE old_id=? AND dirty=false", o.OldID); err != nil {
		return 0, 0, err
	}
	return cursor, selection.duplicates, nil
}

// Each history event views its own span in one transaction-private buffer.
// Exec consumes these bytes before the argument buffer is reused.
func appendChangeRows(args []any, o Owner, input []preparedSample, events []uint8, box *securestore.Box) ([]any, *historyBuffer, error) {
	var header [128]byte
	ownerTail := append(header[:0], `","oldId":`...)
	ownerTail = strconv.AppendInt(ownerTail, o.OldID, 10)
	ownerTail = append(ownerTail, `,"deviceId":`...)
	if o.DeviceID == nil {
		ownerTail = append(ownerTail, "null"...)
	} else {
		ownerTail = strconv.AppendInt(ownerTail, *o.DeviceID, 10)
	}
	ownerTail = append(ownerTail, `,"bindingVersion":`...)
	ownerTail = strconv.AppendInt(ownerTail, o.Epoch, 10)
	ownerTail = append(ownerTail, `,"revision":`...)
	capacity := 0
	for _, index := range events {
		event := &input[index]
		capacity += len(event.payload) + len(event.id) + len(ownerTail) + len(`{"sampleId":"`) + 20 + len(`,"sample":`) + 1
	}
	history := borrowHistoryBuffer(capacity)
	var payloads []byte
	if history == nil {
		payloads = make([]byte, 0, capacity)
	} else {
		payloads = history.bytes[:0]
	}
	ownerID := any(o.OldID)
	for _, index := range events {
		event := &input[index]
		start := len(payloads)
		payloads = append(payloads, `{"sampleId":"`...)
		// IDs are SHA-256 hex; the sample already owns its JSON snapshot.
		payloads = append(payloads, event.id...)
		payloads = append(payloads, ownerTail...)
		payloads = strconv.AppendInt(payloads, event.revision, 10)
		payloads = append(payloads, `,"sample":`...)
		payloads = append(payloads, event.payload...)
		payloads = append(payloads, '}')
		raw := json.RawMessage(payloads[start:len(payloads):len(payloads)])
		stored, err := protectProjection(box, raw, changePayloadScope(event.revision, o.OldID, event.id))
		if err != nil {
			return nil, history, err
		}
		args = append(args, event.revision, ownerID, event.id, stored.JSON, stored.Cipher)
	}
	return args, history, nil
}

func loadStoredSamples(ctx context.Context, tx *sql.Tx, ids []any, minimumVersion, owner int64, box *securestore.Box) (map[string]storedSample, error) {
	// Older stored versions cannot require equality/conflict comparison against
	// any input in this batch. Keep their metadata but avoid serializing and
	// transferring the JSON under the owner lock. Equal/newer versions retain
	// payloads, including mixed-version and out-of-order batches.
	ids[0] = minimumVersion
	rows, err := tx.QueryContext(ctx, batchSQL(batchLookup, len(ids)-1), ids...)
	if err != nil {
		return nil, err
	}
	previous := make(map[string]storedSample, len(ids)-1)
	for rows.Next() {
		var id string
		var item storedSample
		var stored ProjectionPayload
		if err = rows.Scan(&id, &item.version, &item.revision, &stored.JSON, &stored.Cipher); err != nil {
			break
		}
		if item.version >= minimumVersion || stored.Cipher != nil {
			// Authenticate protected rows even when a newer version will replace
			// them. Missing keys or corrupt ciphertext must not allow a rewrite.
			item.payload, err = stored.OpenSample(box, id, owner, item.revision)
			if err != nil {
				break
			}
		}
		previous[id] = item
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return previous, nil
}
