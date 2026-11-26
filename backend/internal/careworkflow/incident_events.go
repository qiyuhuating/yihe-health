package careworkflow

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

type Event struct {
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListEvents returns encrypted notes; only the authorized transport layer
// decrypts them after completing the access audit.
func ListEvents(ctx context.Context, store *sql.Tx, incidentID int64, page, pageSize int) ([]Event, int64, error) {
	var total int64
	if err := store.QueryRowContext(ctx,
		`SELECT count(*) FROM care_incident_event WHERE incident_id=?`, incidentID).Scan(&total); err != nil {
		return nil, 0, err
	}
	items, err := db.QueryList(ctx, store, func(rows *sql.Rows) (Event, error) {
		var item Event
		err := rows.Scan(&item.Actor, &item.Action, &item.Note, &item.CreatedAt)
		return item, err
	}, `SELECT actor,action,note,created_at FROM care_incident_event
      WHERE incident_id=? ORDER BY id ASC LIMIT ? OFFSET ?`, incidentID, pageSize, (page-1)*pageSize)
	return items, total, err
}
