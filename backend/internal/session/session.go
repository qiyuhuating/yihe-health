// Package session provides server-side sessions with request-local verification.
package session

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"sync"
	"time"

	"yihe-care/internal/securestore"
)

const CookieName = "YIHE_SESSION"

type Principal struct {
	Role             string // ADMIN or FAMILY; empty for an anonymous session.
	ID               int64
	Phone            string
	OldID            int64
	DisplayName      string
	FactorGeneration string
}

type Session struct {
	ID        string
	Principal Principal
	CSRFToken string
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	database *databaseStore

	cleanupCancel context.CancelFunc
	cleanupDone   chan struct{}
}

func NewStore(ttl time.Duration) *Store {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{sessions: make(map[string]*Session), ttl: ttl, cleanupCancel: cancel, cleanupDone: make(chan struct{})}
	go func() {
		defer close(s.cleanupDone)
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		s.cleanupLoop(ctx, ticker.C)
	}()
	return s
}

// Close stops maintenance and cancels its in-flight database operation. It is
// idempotent and does not close the caller-owned database or revoke sessions.
func (s *Store) Close() {
	s.cleanupCancel()
	<-s.cleanupDone
}

func token(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Store) backend() *databaseStore {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.database
}

// Create is for callers without a request. HTTP handlers use CreateContext.
func (s *Store) Create(p Principal) *Session {
	return s.CreateContext(context.Background(), p)
}

func (s *Store) CreateContext(ctx context.Context, p Principal) *Session {
	if ctx.Err() != nil {
		return nil
	}
	id, err := token(32)
	if err != nil {
		return nil
	}
	csrf, err := token(32)
	if err != nil {
		return nil
	}
	now := time.Now()
	sess := &Session{ID: id, Principal: p, CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(s.ttl)}
	if database := s.backend(); database != nil {
		if !database.create(ctx, sess) {
			return nil
		}
		return sess
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil
	}
	s.sessions[sess.ID] = sess
	return sess
}

func (s *Store) Get(id string) *Session { return s.GetContext(context.Background(), id) }

func (s *Store) GetContext(ctx context.Context, id string) *Session {
	sess, _ := s.lookup(ctx, id)
	return sess
}

func (s *Store) lookup(ctx context.Context, id string) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	if database := s.backend(); database != nil {
		return database.get(ctx, id)
	}
	s.mu.RLock()
	sess := s.sessions[id]
	s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if sess == nil || time.Now().After(sess.ExpiresAt) {
		return nil, nil
	}
	return sess, nil
}

func (s *Store) Destroy(id string) bool { return s.DestroyContext(context.Background(), id) }

func (s *Store) DestroyContext(ctx context.Context, id string) bool {
	if ctx.Err() != nil {
		return false
	}
	if database := s.backend(); database != nil {
		return database.destroy(ctx, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return false
	}
	delete(s.sessions, id)
	return true
}

// UseDatabase is installed before publishing API handlers. The backend is an
// immutable pair; there is no read-through cache across requests or replicas.
func (s *Store) UseDatabase(database *sql.DB, box *securestore.Box) {
	var backend *databaseStore
	if database != nil {
		backend = &databaseStore{db: database, box: box}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.database = backend
	s.sessions = make(map[string]*Session)
}

func (s *Store) Revoke(role string, id int64) error {
	return s.RevokeContext(context.Background(), role, id)
}

func (s *Store) RevokeContext(ctx context.Context, role string, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if database := s.backend(); database != nil {
		return database.revoke(ctx, role, id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for key, value := range s.sessions {
		if value.Principal.Role == role && value.Principal.ID == id {
			delete(s.sessions, key)
		}
	}
	return nil
}
