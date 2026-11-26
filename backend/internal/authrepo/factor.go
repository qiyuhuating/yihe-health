package authrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"yihe-care/internal/auth"
)

type LoginFactor struct {
	Sealed         string
	Enabled        bool
	LastCounter    int64
	RecoveryHashes []byte
	Generation     string
}
type Factor struct {
	Sealed       string
	Enabled      bool
	PendingUntil time.Time
	LastCounter  int64
}

func LockLoginFactor(ctx context.Context, tx *sql.Tx, role string, actorID int64) (LoginFactor, error) {
	var record LoginFactor
	err := tx.QueryRowContext(ctx,
		"SELECT secret,enabled,last_counter,recovery_hashes,generation FROM auth_factor WHERE role=? AND actor_id=? FOR UPDATE",
		role, actorID).Scan(&record.Sealed, &record.Enabled, &record.LastCounter, &record.RecoveryHashes, &record.Generation)
	return record, err
}

func UpdateLoginFactor(ctx context.Context, tx *sql.Tx, role string, actorID, counter int64, hashes []byte) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE auth_factor SET last_counter=?,recovery_hashes=? WHERE role=? AND actor_id=?",
		counter, json.RawMessage(hashes), role, actorID)
	return err
}

func LockPassword(ctx context.Context, tx *sql.Tx, role string, actorID int64) (string, error) {
	query := "SELECT password FROM users WHERE user_id=? FOR UPDATE"
	if role == auth.RoleFamily {
		query = "SELECT family_password FROM family WHERE family_id=? FOR UPDATE"
	}
	var password string
	err := tx.QueryRowContext(ctx, query, actorID).Scan(&password)
	return password, err
}

func LockFactor(ctx context.Context, tx *sql.Tx, role string, actorID int64) (Factor, error) {
	var factor Factor
	err := tx.QueryRowContext(ctx,
		"SELECT secret,enabled,pending_until,last_counter FROM auth_factor WHERE role=? AND actor_id=? FOR UPDATE",
		role, actorID).Scan(&factor.Sealed, &factor.Enabled, &factor.PendingUntil, &factor.LastCounter)
	return factor, err
}

func SetupFactor(ctx context.Context, tx *sql.Tx, role string, actorID int64, sealed string, until time.Time, generation string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO auth_factor(role,actor_id,secret,pending_until,recovery_hashes,generation)
		 VALUES(?,?,?,?,'[]',?)
		 AS new ON DUPLICATE KEY UPDATE secret=new.secret,pending_until=new.pending_until,last_counter=-1,generation=new.generation`,
		role, actorID, sealed, until, generation)
	return err
}

func EnableFactor(ctx context.Context, tx *sql.Tx, role string, actorID, counter int64, hashes []byte) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE auth_factor SET enabled=true,last_counter=?,recovery_hashes=? WHERE role=? AND actor_id=?",
		counter, json.RawMessage(hashes), role, actorID)
	return err
}
func DeleteFactor(ctx context.Context, tx *sql.Tx, role string, actorID int64) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM auth_factor WHERE role=? AND actor_id=?", role, actorID)
	return err
}
func RevokeDatabaseSessions(ctx context.Context, tx *sql.Tx, role string, actorID int64) error {
	_, err := tx.ExecContext(ctx, "DELETE FROM web_session WHERE role=? AND actor_id=?", role, actorID)
	return err
}
