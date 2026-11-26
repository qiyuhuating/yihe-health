package authrepo

import (
	"context"
	"database/sql"
	"time"

	"yihe-care/internal/db"
)

// LoginAudit is an authorized, non-sensitive audit projection.
type LoginAudit struct {
	ID        int64     `json:"id"`
	Role      string    `json:"role"`
	ActorID   int64     `json:"actorId"`
	Outcome   string    `json:"outcome"`
	RequestID string    `json:"requestId"`
	CreatedAt time.Time `json:"createdAt"`
}

// RecordLoginAttempt writes hashed identifiers in the calling transaction when provided.
func RecordLoginAttempt(ctx context.Context, store *db.Database, tx *sql.Tx, accountHash, ipHash, role string, actorID int64, outcome, requestID string) error {
	const query = "INSERT INTO login_audit(account_hash,ip_hash,role,actor_id,outcome,request_id) VALUES(?,?,?,?,?,?)"
	args := []any{accountHash, ipHash, role, actorID, outcome, requestID}
	if tx != nil {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}
	_, err := store.ExecContext(ctx, query, args...)
	return err
}

// RecentFailureCounts limits the rows read per account/IP and per account.
func RecentFailureCounts(ctx context.Context, store *db.Database, accountHash, ipHash string, since time.Time) (pair, account int, err error) {
	err = store.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM (SELECT 1 FROM login_audit WHERE account_hash=? AND ip_hash=? AND outcome IN ('invalid_credentials','factor_required') AND created_at>? LIMIT 10) pair_attempts),
 (SELECT count(*) FROM (SELECT 1 FROM login_audit WHERE account_hash=? AND outcome IN ('invalid_credentials','factor_required') AND created_at>? LIMIT 30) account_attempts)`,
		accountHash, ipHash, since, accountHash, since).Scan(&pair, &account)
	return
}

// ListLoginAudits returns up to 100 records after a supervisor-authorized cursor.
func ListLoginAudits(ctx context.Context, store db.RowsQuerier, after int64) ([]LoginAudit, error) {
	return db.QueryList(ctx, store, func(rows *sql.Rows) (LoginAudit, error) {
		var item LoginAudit
		err := rows.Scan(&item.ID, &item.Role, &item.ActorID, &item.Outcome, &item.RequestID, &item.CreatedAt)
		return item, err
	}, "SELECT id,role,actor_id,outcome,request_id,created_at FROM login_audit WHERE id>? ORDER BY id LIMIT 100", after)
}
