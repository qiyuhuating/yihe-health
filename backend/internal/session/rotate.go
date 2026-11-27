package session

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"yihe-care/internal/observability"
)

// ErrSessionChanged means a previously presented session cannot be rotated:
// another request consumed or revoked it, or it expired before the commit.
var ErrSessionChanged = errors.New("session changed before rotation")

// RotateContext replaces a presented session with a new random identity.
// On the database backend, replacement and the supplied audit are committed
// together. The callback must use the given transaction, not a second SQL
// connection. Never retry an uncertain commit or issue a cookie before it.
// The development-only memory backend has no shared audit transaction.
func (s *Store) RotateContext(ctx context.Context, oldID string, p Principal, audit func(context.Context, *sql.Tx) error) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if oldID != "" && !validCookieID(oldID) {
		return nil, ErrSessionChanged
	}
	sess, err := s.newSession(p)
	if err != nil {
		return nil, err
	}
	if d := s.backend(); d != nil {
		if err = d.rotate(ctx, oldID, sess, audit); err != nil {
			return nil, err
		}
		return sess, nil
	}
	s.mu.Lock()
	if err = ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if oldID != "" {
		old := s.sessions[oldID]
		if old == nil || time.Now().After(old.ExpiresAt) {
			s.mu.Unlock()
			return nil, ErrSessionChanged
		}
		delete(s.sessions, oldID)
	}
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
	if audit != nil {
		if err = audit(ctx, nil); err != nil {
			s.DestroyContext(context.WithoutCancel(ctx), sess.ID)
			return nil, err
		}
	}
	return sess, nil
}

func (d *databaseStore) rotate(ctx context.Context, oldID string, sess *Session, audit func(context.Context, *sql.Tx) error) error {
	sealed, err := d.sealSession(sess)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	trace := observability.TransactionTraceFrom(ctx)
	start := trace.Start()
	tx, err := d.db.BeginTx(ctx, nil)
	trace.End("pool_begin", start)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	start = trace.Start()
	args := []any{idHash(sess.ID), sess.Principal.Role, sess.Principal.ID, sealed, sess.ExpiresAt.UTC()}
	if oldID == "" {
		if _, err = tx.ExecContext(ctx, insertSessionSQL, args...); err != nil {
			return err
		}
	} else {
		// The primary-key change consumes the old cookie in the same write.
		// Only one replica can win; missing or expired rows fail closed.
		result, err := tx.ExecContext(ctx, "UPDATE web_session SET id_hash=?,role=?,actor_id=?,payload=?,expires_at=? WHERE id_hash=? AND expires_at>UTC_TIMESTAMP(6)", append(args, idHash(oldID))...)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrSessionChanged
		}
	}
	if audit != nil {
		if err = audit(ctx, tx); err != nil {
			return err
		}
	}
	trace.End("business_sql", start)
	start = trace.Start()
	err = tx.Commit()
	trace.End("commit", start)
	return err
}
