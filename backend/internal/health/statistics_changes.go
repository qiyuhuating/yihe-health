package health

import (
	"context"
	"database/sql"
	"errors"

	fastjson "github.com/goccy/go-json"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

const statisticsChangeLimit = 256

// StatisticsChanges reads a bounded committed owner history. Revision ranges
// are globally allocated, so gaps in the owner's revisions are legitimate.
// Every event must match its indexed identity and the final event must reach
// the caller's observed clock. The caller rechecks that clock before use.
func StatisticsChanges(ctx context.Context, store db.RowsQuerier, owner, after, through int64, box *securestore.Box) ([]Record, error) {
	if after <= 0 || through <= after || through > MaxRevision {
		return nil, ErrInvalidChangeRevision
	}
	var records []Record
	err := db.QueryEach(ctx, store, func(rows *sql.Rows) error {
		var revision int64
		var id string
		var stored ProjectionPayload
		if err := rows.Scan(&revision, &id, &stored.JSON, &stored.Cipher); err != nil {
			return err
		}
		if len(records) == statisticsChangeLimit {
			return errors.New("statistics change budget exceeded")
		}
		payload, err := stored.Open(box, changePayloadScope(revision, owner, id))
		if err != nil {
			return err
		}
		var record Record
		if err := fastjson.Unmarshal(payload, &record); err != nil {
			return err
		}
		if record.OldID != owner || record.Revision != revision || record.SampleID != id {
			return errors.New("statistics change identity mismatch")
		}
		records = append(records, record)
		return nil
	}, "SELECT revision,sample_id,payload,payload_cipher FROM health_change WHERE old_id=? AND revision>? AND revision<=? ORDER BY revision LIMIT 257",
		owner, after, through)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 || records[len(records)-1].Revision != through {
		return nil, ErrInvalidChangeRevision
	}
	return records, nil
}
