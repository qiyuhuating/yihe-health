package session

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/securestore"
)

// Exercise database/sql's context propagation without network timing or a live
// server. The existing integration suite still verifies encrypted SQL sessions.
type contextConnector struct {
	started chan context.Context
	exec    func(context.Context) (driver.Result, error)
}

func (c *contextConnector) Connect(context.Context) (driver.Conn, error) { return &contextConn{c}, nil }
func (c *contextConnector) Driver() driver.Driver                        { return contextDriver{c} }

type contextDriver struct{ c *contextConnector }

func (d contextDriver) Open(string) (driver.Conn, error) { return &contextConn{d.c}, nil }

type contextConn struct{ c *contextConnector }

func (c *contextConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *contextConn) Begin() (driver.Tx, error)      { return nil, errors.New("unexpected transaction") }
func (c *contextConn) Close() error                   { return nil }
func (c *contextConn) Ping(ctx context.Context) error { return ctx.Err() }
func (c *contextConn) block(ctx context.Context) error {
	c.c.started <- ctx
	<-ctx.Done()
	return ctx.Err()
}
func (c *contextConn) QueryContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return nil, c.block(ctx)
}
func (c *contextConn) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	if c.c.exec != nil {
		return c.c.exec(ctx)
	}
	return nil, c.block(ctx)
}

func contextDatabase(t *testing.T, connector *contextConnector) (*sql.DB, *securestore.Box) {
	t.Helper()
	pool := sql.OpenDB(connector)
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { pool.Close() })
	box, err := securestore.New(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return pool, box
}

func TestSessionIOFollowsCallerCancellation(t *testing.T) {
	for _, op := range []struct {
		name string
		call func(context.Context, *Store) bool
	}{
		{"get", func(ctx context.Context, s *Store) bool { return s.GetContext(ctx, "synthetic-id") == nil }},
		{"create", func(ctx context.Context, s *Store) bool { return s.CreateContext(ctx, Principal{}) == nil }},
		{"destroy", func(ctx context.Context, s *Store) bool { return !s.DestroyContext(ctx, "synthetic-id") }},
		{"revoke", func(ctx context.Context, s *Store) bool {
			return errors.Is(s.RevokeContext(ctx, "ADMIN", 1), context.Canceled)
		}},
		{"request", func(ctx context.Context, s *Store) bool {
			r := httptest.NewRequest("POST", "/test", nil).WithContext(ctx)
			r.AddCookie(&http.Cookie{Name: CookieName, Value: strings.Repeat("a", 64)})
			return s.FromRequest(r) == nil
		}},
	} {
		t.Run(op.name, func(t *testing.T) {
			connector := &contextConnector{started: make(chan context.Context, 1)}
			pool, box := contextDatabase(t, connector)
			store := NewStore(time.Hour)
			t.Cleanup(store.Close)
			store.UseDatabase(pool, box)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan bool, 1)
			go func() { done <- op.call(ctx, store) }()
			var active context.Context
			select {
			case active = <-connector.started:
			case <-time.After(5 * time.Second):
				t.Fatal("operation did not reach driver")
			}
			if _, ok := active.Deadline(); !ok {
				t.Fatal("operation lacks the storage timeout")
			}
			cancel()
			select {
			case failed := <-done:
				if !failed || !errors.Is(active.Err(), context.Canceled) {
					t.Fatalf("request cancellation lost: result=%v context=%v", failed, active.Err())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled operation retained the connection")
			}
			if pool.Stats().InUse != 0 {
				t.Fatal("cancelled operation still owns SQL connection")
			}
		})
	}
}

func TestCancelledMemoryOperationsDoNotMutateSessions(t *testing.T) {
	s := NewStore(time.Hour)
	t.Cleanup(s.Close)
	account := s.Create(Principal{Role: "ADMIN", ID: 1})
	if account == nil || account.ExpiresAt.Sub(account.CreatedAt) != time.Hour {
		t.Fatal("invalid session lifetime")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s.CreateContext(ctx, Principal{}) != nil || s.GetContext(ctx, account.ID) != nil || s.DestroyContext(ctx, account.ID) || !errors.Is(s.RevokeContext(ctx, "ADMIN", 1), context.Canceled) {
		t.Fatal("cancelled operation succeeded")
	}
	if s.Get(account.ID) != account {
		t.Fatal("cancelled write changed existing session")
	}
	r := httptest.NewRequest("GET", "/test", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: account.ID})
	if s.FromRequest(r) != account {
		t.Fatal("request verification failed")
	}
	cachedContext, cancelCached := context.WithCancel(r.Context())
	cancelCached()
	if s.FromRequest(r.WithContext(cachedContext)) != nil {
		t.Fatal("cancelled request reused verified snapshot")
	}
}

func TestStoreCloseIsConcurrentAndDoesNotRevoke(t *testing.T) {
	s := NewStore(time.Hour)
	t.Cleanup(s.Close)
	account := s.Create(Principal{Role: "ADMIN", ID: 1})
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() { defer workers.Done(); s.Close() }()
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup worker did not exit")
	}
	if s.Get(account.ID) != account {
		t.Fatal("Close revoked a valid session")
	}
}

func TestCloseCancelsActiveCleanupAndLeavesCallerDatabaseOpen(t *testing.T) {
	connector := &contextConnector{started: make(chan context.Context, 1)}
	pool, box := contextDatabase(t, connector)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{sessions: make(map[string]*Session), cleanupCancel: cancel, cleanupDone: make(chan struct{})}
	s.UseDatabase(pool, box)
	t.Cleanup(s.Close)
	ticks := make(chan time.Time, 1)
	go func() { defer close(s.cleanupDone); s.cleanupLoop(ctx, ticks) }()
	ticks <- time.Now()
	var active context.Context
	select {
	case active = <-connector.started:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not run")
	}
	done := make(chan struct{})
	go func() { s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not cancel cleanup SQL")
	}
	if !errors.Is(active.Err(), context.Canceled) {
		t.Fatal("cleanup did not inherit lifecycle cancellation")
	}
	if err := pool.PingContext(context.Background()); err != nil {
		t.Fatal("Close shut down caller database", err)
	}
}

func TestCleanupContinuesAfterFailureAndExpiresOnlyOldMemorySessions(t *testing.T) {
	attempts := make(chan struct{}, 2)
	connector := &contextConnector{exec: func(context.Context) (driver.Result, error) {
		attempts <- struct{}{}
		return nil, errors.New("synthetic unavailable")
	}}
	pool, box := contextDatabase(t, connector)
	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{sessions: make(map[string]*Session), cleanupCancel: cancel, cleanupDone: make(chan struct{})}
	s.UseDatabase(pool, box)
	t.Cleanup(s.Close)
	ticks := make(chan time.Time, 1)
	go func() { defer close(s.cleanupDone); s.cleanupLoop(ctx, ticks) }()
	for range 2 {
		ticks <- time.Now()
		select {
		case <-attempts:
		case <-time.After(5 * time.Second):
			t.Fatal("failed cleanup stopped the loop")
		}
	}
	s.Close()
	memory := NewStore(time.Hour)
	t.Cleanup(memory.Close)
	old := memory.Create(Principal{})
	current := memory.Create(Principal{})
	now := time.Now()
	old.ExpiresAt = now.Add(-time.Second)
	if err := memory.deleteExpired(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if memory.Get(old.ID) != nil || memory.Get(current.ID) != current {
		t.Fatal("cleanup removed a current session or retained an expired one")
	}
	if _, retained := memory.sessions[old.ID]; retained {
		t.Fatal("expired session still occupies the memory store")
	}
}
