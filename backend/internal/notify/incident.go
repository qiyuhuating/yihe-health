package notify

import (
	"context"
	"database/sql"

	"yihe-care/internal/securestore"
)

// EnqueueIncident writes both the care SMS outbox and the in-app inbox
// under the same caller-owned incident transaction. Acknowledgement must
// occur only after that transaction commits.
func EnqueueIncident(ctx context.Context, tx *sql.Tx, box *securestore.Box, incidentID, residentID int64) (string, error) {
	count, err := QueueCareSMS(ctx, tx, box, incidentID, residentID)
	if err != nil {
		return "", err
	}
	if err := CareInbox(ctx, tx, box, residentID, incidentID); err != nil {
		return "", err
	}
	if count > 0 {
		return "pending", nil
	}
	return "no_recipient", nil
}
