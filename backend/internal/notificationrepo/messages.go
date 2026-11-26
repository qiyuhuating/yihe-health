package notificationrepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

type Message struct {
	ID        int64     `json:"id"`
	Topic     string    `json:"topic"`
	SourceID  string    `json:"sourceId"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
}

// List reads messages only from the family scope selected by the workflow.
func List(ctx context.Context, tx *sql.Tx, familyID, after int64) ([]Message, error) {
	return db.QueryList(ctx, tx, func(rows *sql.Rows) (Message, error) {
		var m Message
		err := rows.Scan(&m.ID, &m.Topic, &m.SourceID, &m.State, &m.CreatedAt)
		return m, err
	}, "SELECT id,topic,source_key,status,created_at FROM message_outbox WHERE family_id=? AND channel='in_app' AND id>? ORDER BY id LIMIT 100", familyID, after)
}
