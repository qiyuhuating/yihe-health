package session

import (
	"context"
	"log/slog"
	"time"
)

func (s *Store) cleanupLoop(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if err := s.deleteExpired(ctx, time.Now()); err != nil && ctx.Err() == nil {
				// Record the failed maintenance cycle without session identifiers,
				// payloads, credentials or a database error string in the log.
				slog.Warn("session_cleanup_failed")
			}
		}
	}
}

func (s *Store) deleteExpired(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if database := s.backend(); database != nil {
		return database.deleteExpired(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
	return nil
}
