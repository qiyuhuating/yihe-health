package health

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

var ErrLatestTooManySeries = errors.New("health latest candidates exceed 10000")

type latestQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func Latest(ctx context.Context, store *db.Database, oldID int64, box *securestore.Box) ([]Record, error) {
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := LatestInTransaction(ctx, tx, oldID, box)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// LatestInTransaction reuses the caller's authorized transaction. A cold
// projection is built under the owner lock; no second pool checkout is needed.
func LatestInTransaction(ctx context.Context, tx *sql.Tx, oldID int64, box *securestore.Box) ([]Record, error) {
	items, ready, err := readReadyLatest(ctx, tx, oldID, box)
	if err != nil || ready {
		return items, err
	}
	if _, err = LockOwner(ctx, tx, oldID); err != nil {
		return nil, err
	}
	items, ready, err = readReadyLatest(ctx, tx, oldID, box)
	if err != nil || ready {
		return items, err
	}
	items, err = buildLatest(ctx, tx, oldID, box)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		payload, marshalErr := json.Marshal(item.Sample)
		if marshalErr != nil {
			return nil, marshalErr
		}
		if err = insertLatest(ctx, tx, item, payload, box); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO health_latest_ready(old_id) VALUES(?)", oldID); err != nil {
		return nil, err
	}
	return items, nil
}

// A ready owner with no live samples returns one NULL joined row. An
// uninitialized owner returns no rows, so the warm path needs one query.
func readReadyLatest(ctx context.Context, q latestQuerier, oldID int64, box *securestore.Box) ([]Record, bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT LOWER(HEX(l.sample_id)),l.device_id,COALESCE(l.epoch,0),COALESCE(l.revision,0),l.series_key,l.payload,l.payload_cipher
 FROM health_latest_ready r LEFT JOIN health_sample_latest l ON l.old_id=r.old_id WHERE r.old_id=? LIMIT 10001`, oldID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]Record, 0, 16)
	ready := false
	for rows.Next() {
		ready = true
		if len(items) == 10000 {
			return nil, false, ErrLatestTooManySeries
		}
		rec := Record{OldID: oldID}
		var id sql.NullString
		var series []byte
		var stored ProjectionPayload
		if err = rows.Scan(&id, &rec.DeviceID, &rec.Epoch, &rec.Revision, &series, &stored.JSON, &stored.Cipher); err != nil {
			return nil, false, err
		}
		if !id.Valid {
			continue
		}
		rec.SampleID = id.String
		payload, err := stored.Open(box, latestPayloadScope(oldID, series, rec.Revision))
		if err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal(payload, &rec.Sample); err != nil {
			return nil, false, err
		}
		items = append(items, rec)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	sortLatest(items)
	return items, ready, nil
}

func buildLatest(ctx context.Context, q *sql.Tx, oldID int64, box *securestore.Box) ([]Record, error) {
	partition := "BINARY source,BINARY source_device,BINARY metric,BINARY semantics"
	// The SQL index stores microseconds, but the authenticated payload keeps
	// nanoseconds. Read all rows at each series' maximum indexed time, then
	// resolve the exact timestamp and revision in Go. Bound distinct series,
	// not tied rows, and avoid an unnecessary global sort of sample IDs.
	sql := "SELECT LOWER(HEX(s.sample_id)),s.device_id,s.epoch,s.revision,s.payload," + AliasedSampleCipherSQL + " FROM health_sample s INNER JOIN (" +
		"SELECT sample_id,RANK() OVER (PARTITION BY " + partition + " ORDER BY sample_start DESC) AS latest_rank " +
		"FROM health_sample WHERE old_id=? AND NOT deleted) ranked ON ranked.sample_id=s.sample_id " +
		"WHERE ranked.latest_rank=1"
	rows, err := q.QueryContext(ctx, sql, oldID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type series struct{ source, device, metric, semantics string }
	selected := make(map[series]Record)
	for rows.Next() {
		rec := Record{OldID: oldID}
		var stored ProjectionPayload
		if err = rows.Scan(&rec.SampleID, &rec.DeviceID, &rec.Epoch, &rec.Revision, &stored.JSON, &stored.Cipher); err != nil {
			return nil, err
		}
		payload, err := stored.OpenSample(box, rec.SampleID, oldID, rec.Revision)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(payload, &rec.Sample); err != nil {
			return nil, err
		}
		s := rec.Sample
		key := series{s.Source, s.SourceDevice, s.Metric, s.Semantics}
		prior, exists := selected[key]
		if !exists && len(selected) == 10000 {
			return nil, ErrLatestTooManySeries
		}
		if !exists || s.Start.After(prior.Sample.Start) || (s.Start.Equal(prior.Sample.Start) && rec.Revision > prior.Revision) {
			selected[key] = rec
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	items := make([]Record, 0, len(selected))
	for _, rec := range selected {
		items = append(items, rec)
	}
	sortLatest(items)
	return items, nil
}

func sortLatest(items []Record) {
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i].Sample, items[j].Sample
		return cmp.Or(cmp.Compare(a.Source, b.Source), cmp.Compare(a.SourceDevice, b.SourceDevice), cmp.Compare(a.Metric, b.Metric), cmp.Compare(a.Semantics, b.Semantics)) < 0
	})
}

func latestKey(s Sample) [32]byte {
	var scratch [256]byte
	buf := scratch[:0]
	for _, part := range [...]string{s.Source, s.SourceDevice, s.Metric, s.Semantics} {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(part)))
		buf = append(buf, part...)
	}
	return sha256.Sum256(buf)
}

func insertLatest(ctx context.Context, tx *sql.Tx, rec Record, payload []byte, box *securestore.Box) error {
	key := latestKey(rec.Sample)
	stored, err := protectProjection(box, payload, latestPayloadScope(rec.OldID, key[:], rec.Revision))
	if err != nil {
		return err
	}
	sampleKey, err := hex.DecodeString(rec.SampleID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO health_sample_latest(old_id,series_key,sample_id,device_id,epoch,revision,payload,payload_cipher) VALUES(?,?,?,?,?,?,?,?)",
		rec.OldID, key[:], sampleKey, rec.DeviceID, rec.Epoch, rec.Revision, stored.jsonArgument(), stored.Cipher)
	return err
}

type latestPrior struct {
	record  Record
	payload ProjectionPayload
}

func refreshLatest(ctx context.Context, tx *sql.Tx, o Owner, input []preparedSample, final []uint8, box *securestore.Box) error {
	type series struct{ source, device, metric, semantics string }
	seen := make(map[series]int, min(len(final), 8))
	indices := make([]uint8, 0, min(len(final), 8))
	keys := make([][32]byte, 0, min(len(final), 8))
	for _, index := range final {
		sample := input[index].sample
		identity := series{sample.Source, sample.SourceDevice, sample.Metric, sample.Semantics}
		if slot, exists := seen[identity]; exists {
			prior := &input[indices[slot]]
			if !sample.Deleted && (prior.sample.Deleted || sample.Start.After(prior.sample.Start) ||
				(sample.Start.Equal(prior.sample.Start) && input[index].revision > prior.revision)) {
				indices[slot] = index
			}
			continue
		}
		seen[identity] = len(indices)
		indices = append(indices, index)
		keys = append(keys, latestKey(sample))
	}
	// The owner-clock lock already serializes initial projection construction
	// and sample writes. Read the ready marker and lock the affected prior rows
	// together instead of making a separate round trip for every series.
	args := make([]any, 0, len(keys)+1)
	for i := range keys {
		args = append(args, keys[i][:])
	}
	args = append(args, o.OldID)
	rows, err := tx.QueryContext(ctx, "SELECT l.series_key,LOWER(HEX(l.sample_id)),l.device_id,COALESCE(l.epoch,0),COALESCE(l.revision,0),l.payload,l.payload_cipher"+
		" FROM health_latest_ready r LEFT JOIN health_sample_latest l ON l.old_id=r.old_id AND l.series_key IN ("+
		strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")+") WHERE r.old_id=? FOR UPDATE", args...)
	if err != nil {
		return err
	}
	ready := false
	priors := make(map[[32]byte]latestPrior, len(keys))
	for rows.Next() {
		ready = true
		var key []byte
		var id sql.NullString
		var prior latestPrior
		if err = rows.Scan(&key, &id, &prior.record.DeviceID, &prior.record.Epoch, &prior.record.Revision, &prior.payload.JSON, &prior.payload.Cipher); err != nil {
			break
		}
		if id.Valid {
			prior.record.SampleID = id.String
			priors[[32]byte(key)] = prior
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil || !ready {
		return err
	}
	// Resolve all winners under the same owner lock, then write only changed
	// projections. Multiple series share one upsert and one delete statement.
	upserts := make([]any, 0, len(keys)*8)
	deletes := []any{o.OldID}
	for i, index := range indices {
		prior, found := priors[keys[i]]
		change, err := refreshLatestSeries(ctx, tx, o, &input[index], keys[i], prior, found, box)
		if err != nil {
			return err
		}
		if !change.changed {
			continue
		}
		if change.winner == nil {
			deletes = append(deletes, keys[i][:])
			continue
		}
		rec := change.winner
		sampleKey, err := hex.DecodeString(rec.SampleID)
		if err != nil {
			return err
		}
		upserts = append(upserts, o.OldID, keys[i][:], sampleKey, rec.DeviceID, rec.Epoch, rec.Revision, change.payload.jsonArgument(), change.payload.Cipher)
	}
	if len(deletes) > 1 {
		if _, err = tx.ExecContext(ctx, "DELETE FROM health_sample_latest WHERE old_id=? AND series_key IN ("+
			strings.TrimSuffix(strings.Repeat("?,", len(deletes)-1), ",")+")", deletes...); err != nil {
			return err
		}
	}
	if len(upserts) != 0 {
		_, err = tx.ExecContext(ctx, batchSQL(batchLatest, len(upserts)/8), upserts...)
	}
	return err
}

type latestChange struct {
	winner  *Record
	payload ProjectionPayload
	changed bool
}

func refreshLatestSeries(ctx context.Context, tx *sql.Tx, owner Owner, item *preparedSample, key [32]byte, previous latestPrior, found bool, box *securestore.Box) (latestChange, error) {
	oldID, s := owner.OldID, item.sample
	prior, old := previous.record, previous.payload
	oldPayload, err := old.Open(box, latestPayloadScope(oldID, key[:], prior.Revision))
	if err != nil {
		return latestChange{}, err
	}
	// A ready, authenticated prior winner bounds every unchanged row in this
	// series. The best newly written candidate can replace that bound when
	// its exact timestamp/revision wins, even within the same SQL microsecond.
	// Backward corrections and deletions still scan the stored candidates.
	// The winning new row was sealed and written in this transaction.
	if found && old.Cipher != nil && box != nil && !s.Deleted {
		var previousSample Sample
		if json.Unmarshal(oldPayload, &previousSample) == nil && !previousSample.Deleted &&
			latestKey(previousSample) == key && safeSampleTime(s.Start) &&
			(s.Start.After(previousSample.Start) || (s.Start.Equal(previousSample.Start) && item.revision > prior.Revision)) {
			stored, err := protectProjection(box, item.payload, latestPayloadScope(oldID, key[:], item.revision))
			if err != nil {
				return latestChange{}, err
			}
			return latestChange{winner: &Record{SampleID: item.id, OldID: oldID, DeviceID: owner.DeviceID,
				Epoch: owner.Epoch, Revision: item.revision, Sample: s}, payload: stored, changed: true}, nil
		}
	}
	// The coarse predicates permit the existing prefix index to narrow the
	// lookup; binary predicates preserve the exact series identity. Only the
	// maximum indexed microsecond is transferred, including all ties for
	// the exact nanosecond/revision comparison below.
	filter := "source=? AND source_device=? AND metric=? AND semantics=? AND BINARY source=BINARY ? AND BINARY source_device=BINARY ? AND BINARY metric=BINARY ? AND BINARY semantics=BINARY ?"
	query := "SELECT LOWER(HEX(sample_id)),device_id,epoch,revision,payload," + SampleCipherSQL + " FROM health_sample WHERE old_id=? AND sample_start=(" +
		"SELECT sample_start FROM health_sample WHERE old_id=? AND " + filter +
		" AND NOT deleted ORDER BY sample_start DESC LIMIT 1) AND " + filter + " AND NOT deleted ORDER BY revision DESC,sample_id DESC"
	rows, err := tx.QueryContext(ctx, query, oldID, oldID,
		s.Source, s.SourceDevice, s.Metric, s.Semantics, s.Source, s.SourceDevice, s.Metric, s.Semantics,
		s.Source, s.SourceDevice, s.Metric, s.Semantics, s.Source, s.SourceDevice, s.Metric, s.Semantics)
	if err != nil {
		return latestChange{}, err
	}
	var winner *Record
	var winnerPayload []byte
	for rows.Next() {
		rec := Record{OldID: oldID}
		var stored ProjectionPayload
		if err = rows.Scan(&rec.SampleID, &rec.DeviceID, &rec.Epoch, &rec.Revision, &stored.JSON, &stored.Cipher); err != nil {
			break
		}
		var payload []byte
		payload, err = stored.OpenSample(box, rec.SampleID, oldID, rec.Revision)
		if err != nil {
			break
		}
		if err = json.Unmarshal(payload, &rec.Sample); err != nil {
			break
		}
		if winner == nil || rec.Sample.Start.After(winner.Sample.Start) || (rec.Sample.Start.Equal(winner.Sample.Start) && rec.Revision > winner.Revision) {
			copyRec := rec
			winner = &copyRec
			winnerPayload = payload
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return latestChange{}, err
	}
	if winner == nil {
		return latestChange{changed: found}, nil
	}
	// Keep a byte-identical winner with matching metadata and encryption key.
	// An older sample or unrelated correction need not rewrite the latest
	// index or generate fresh redo. A key rotation still rewrites the row.
	activeEnvelope := old.Cipher == nil && box == nil
	if old.Cipher != nil && box != nil {
		active := box.ActiveKeyID()
		if active == "" {
			activeEnvelope = !strings.HasPrefix(*old.Cipher, securestore.Prefix+"v2:")
		} else {
			activeEnvelope = strings.HasPrefix(*old.Cipher, securestore.Prefix+"v2:"+active+":")
		}
	}
	sameDevice := prior.DeviceID == nil && winner.DeviceID == nil ||
		prior.DeviceID != nil && winner.DeviceID != nil && *prior.DeviceID == *winner.DeviceID
	if found && activeEnvelope && sameDevice && prior.SampleID == winner.SampleID &&
		prior.Revision == winner.Revision && prior.Epoch == winner.Epoch && bytes.Equal(oldPayload, winnerPayload) {
		return latestChange{}, nil
	}
	stored, err := protectProjection(box, winnerPayload, latestPayloadScope(oldID, key[:], winner.Revision))
	if err != nil {
		return latestChange{}, err
	}
	return latestChange{winner: winner, payload: stored, changed: true}, nil
}
