package health

import (
	"context"
	"encoding/json"
	"errors"

	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

var ErrInvalidChangeRevision = errors.New("health change revision out of range")

type ChangesPage struct {
	Items      []json.RawMessage
	NextCursor int64
	HasMore    bool
}

// ListChanges reads one extra revision to determine whether more changes remain.
func ListChanges(ctx context.Context, store db.RowsQuerier, residentID, after int64, limit int, box *securestore.Box) (ChangesPage, error) {
	rows, err := store.QueryContext(ctx,
		`SELECT revision,LOWER(HEX(sample_id)),payload,payload_cipher FROM health_change WHERE old_id=? AND revision>? ORDER BY revision LIMIT ?`,
		residentID, after, limit+1)
	if err != nil {
		return ChangesPage{}, err
	}
	defer rows.Close()

	page := ChangesPage{Items: []json.RawMessage{}, NextCursor: after}
	for rows.Next() {
		var revision int64
		var id string
		var stored ProjectionPayload
		if err := rows.Scan(&revision, &id, &stored.JSON, &stored.Cipher); err != nil {
			return ChangesPage{}, err
		}
		if revision < 0 || revision > MaxRevision {
			return ChangesPage{}, ErrInvalidChangeRevision
		}
		if len(page.Items) == limit {
			page.HasMore = true
			break
		}
		payload, err := stored.Open(box, changePayloadScope(revision, residentID, id))
		if err != nil {
			return ChangesPage{}, err
		}
		page.Items = append(page.Items, payload)
		page.NextCursor = revision
	}
	if err := rows.Err(); err != nil {
		return ChangesPage{}, err
	}
	return page, nil
}
