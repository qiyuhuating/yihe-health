package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
)

type cacheTestWire struct {
	lastOptions driver.TxOptions
	mysqlWireConn
	prepared, executed, direct, closed, connClosed int
	maxOpen                                        int
	prepareErr, executeErr                         error
}

func (c *cacheTestWire) PrepareContext(ctx context.Context, _ string) (driver.Stmt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.maxOpen > 0 && c.prepared-c.closed >= c.maxOpen {
		return nil, errors.New("server statement budget exhausted")
	}
	c.prepared++
	if c.prepareErr != nil {
		return nil, c.prepareErr
	}
	return &cacheTestStmt{owner: c}, nil
}
func (c *cacheTestWire) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.direct++
	return driver.RowsAffected(1), nil
}
func (c *cacheTestWire) Close() error { c.connClosed++; return nil }

type cacheTestStmt struct{ owner *cacheTestWire }

func (s *cacheTestStmt) Close() error                             { s.owner.closed++; return nil }
func (*cacheTestStmt) NumInput() int                              { return 1 }
func (*cacheTestStmt) Exec([]driver.Value) (driver.Result, error) { panic("legacy execution") }
func (*cacheTestStmt) Query([]driver.Value) (driver.Rows, error)  { panic("legacy query") }
func (s *cacheTestStmt) ExecContext(ctx context.Context, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.owner.executed++
	return driver.RowsAffected(1), s.owner.executeErr
}
func (*cacheTestStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	panic("unused query")
}
func cacheForTest(w *cacheTestWire, capacity int) *statementConn {
	return &statementConn{mysqlWireConn: w, plans: make(map[string]mysqlWireStmt), keys: make([]string, capacity)}
}
func TestStatementCacheReuseFIFOAndArgumentIsolation(t *testing.T) {
	w := &cacheTestWire{}
	c := cacheForTest(w, 2)
	ctx := context.Background()
	exec := func(q string, n int) {
		t.Helper()
		if _, err := c.ExecContext(ctx, q, []driver.NamedValue{{Ordinal: 1, Value: int64(n)}}); err != nil {
			t.Fatal(err)
		}
	}
	exec("SELECT ?", 1)
	exec("SELECT ?", 2)
	if w.prepared != 1 || w.executed != 2 {
		t.Fatal("did not reuse handle", w)
	}
	exec("SELECT ?+1", 3)
	exec("SELECT ?+2", 4)
	if len(c.plans) != 2 || w.closed != 1 {
		t.Fatal("cache not bounded", w)
	}
	exec("SELECT ?", 5)
	if w.prepared != 4 || w.closed != 2 {
		t.Fatal("FIFO did not evict complete key", w)
	}
	if _, err := c.ExecContext(ctx, "SELECT ?", []driver.NamedValue{{Ordinal: 1}, {Ordinal: 2}}); err == nil || w.executed != 5 {
		t.Fatal("argument count not checked before execute", err, w)
	}
	if _, err := c.ExecContext(ctx, "DO 1", nil); err != nil || w.direct != 1 {
		t.Fatal("parameterless statements should remain direct", err)
	}
	if err := c.Close(); err != nil || w.connClosed != 1 || c.plans != nil {
		t.Fatal("connection close failed", err)
	}
}
func TestStatementCacheNeverReplaysExecutionErrors(t *testing.T) {
	sentinel := errors.New("uncertain execution")
	for _, err := range []error{sentinel, &mysql.MySQLError{Number: 1295}, driver.ErrBadConn} {
		w := &cacheTestWire{executeErr: err}
		c := cacheForTest(w, 2)
		_, got := c.ExecContext(context.Background(), "INSERT INTO t VALUES(?)", []driver.NamedValue{{Ordinal: 1, Value: int64(1)}})
		if !errors.Is(got, err) || w.executed != 1 || w.direct != 0 {
			t.Fatal("execution error retried", got, w)
		}
	}
	w := &cacheTestWire{prepareErr: &mysql.MySQLError{Number: 1295}}
	c := cacheForTest(w, 2)
	if _, err := c.ExecContext(context.Background(), "SELECT unsupported(?)", []driver.NamedValue{{Ordinal: 1}}); err != nil || w.direct != 1 || w.executed != 0 {
		t.Fatal("prepare-only fallback failed", err, w)
	}
}
func TestStatementCacheCancellationAndInvalidPreparation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := &cacheTestWire{}
	c := cacheForTest(w, 2)
	if _, err := c.ExecContext(ctx, "SELECT ?", []driver.NamedValue{{Ordinal: 1}}); !errors.Is(err, context.Canceled) || w.executed != 0 {
		t.Fatal(err, w)
	}
	sentinel := errors.New("prepare failure")
	w.prepareErr = sentinel
	if _, err := c.ExecContext(context.Background(), "SELECT ?", []driver.NamedValue{{Ordinal: 1}}); !errors.Is(err, sentinel) || w.direct != 0 || len(c.plans) != 0 {
		t.Fatal("prepare error swallowed", err, w)
	}
}
func TestMySQLStatementCacheLimit(t *testing.T) {
	for raw, want := range map[string]int{"": 64, " 0 ": 0, "1": 1, "128": 128} {
		if got, err := mysqlStatementCacheLimit(raw); err != nil || got != want {
			t.Fatal(raw, got, err)
		}
	}
	for _, raw := range []string{"-1", "129", "abc", "1.5"} {
		if _, err := mysqlStatementCacheLimit(raw); err == nil {
			t.Fatal("accepted invalid size", raw)
		}
	}
}

func TestStatementCacheSessionChangeInvalidatesHandles(t *testing.T) {
	w := &cacheTestWire{}
	c := cacheForTest(w, 2)
	ctx := context.Background()
	args := []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}
	c.ExecContext(ctx, "SELECT ?", args)
	c.ExecContext(ctx, "/* session */ SET sql_mode=?", args)
	if w.closed != 1 || w.direct != 1 || len(c.plans) != 0 {
		t.Fatal("session mutation retained prepared statements", w)
	}
	c.ExecContext(ctx, "SELECT ?", args)
	if w.prepared != 2 {
		t.Fatal("statement not reprepared after session change", w)
	}
}

func TestStatementCacheEvictsBeforeReachingServerBudget(t *testing.T) {
	w := &cacheTestWire{maxOpen: 1}
	c := cacheForTest(w, 1)
	for _, q := range []string{"SELECT ?", "SELECT ?+1", "SELECT ?+2"} {
		if _, err := c.ExecContext(context.Background(), q, []driver.NamedValue{{Ordinal: 1, Value: int64(1)}}); err != nil {
			t.Fatal("replacement exceeded bounded server budget", err)
		}
	}
	if w.prepared != 3 || w.closed != 2 {
		t.Fatal(w)
	}
}

func (w *cacheTestWire) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	w.lastOptions = opts
	return nil, nil
}
func TestKnownSessionIsolationAvoidsRedundantSetAndDisablesOnMutation(t *testing.T) {
	w := &cacheTestWire{}
	c := cacheForTest(w, 2)
	c.readCommittedSession = true
	opts := driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelReadCommitted), ReadOnly: true}
	c.BeginTx(context.Background(), opts)
	if w.lastOptions.Isolation != driver.IsolationLevel(sql.LevelDefault) || !w.lastOptions.ReadOnly {
		t.Fatal("known session or readonly options changed", w.lastOptions)
	}
	c.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(sql.LevelSerializable)})
	if w.lastOptions.Isolation != driver.IsolationLevel(sql.LevelSerializable) {
		t.Fatal("overrode caller isolation")
	}
	c.ExecContext(context.Background(), "SET SESSION transaction_isolation='REPEATABLE-READ'", nil)
	c.BeginTx(context.Background(), opts)
	if w.lastOptions.Isolation != opts.Isolation {
		t.Fatal("session mutation did not restore explicit isolation")
	}
	for _, q := range []string{"/* session */ SET sql_mode=''", "CALL session_change()"} {
		c.readCommittedSession = true
		c.PrepareContext(context.Background(), q)
		if c.readCommittedSession {
			t.Fatal("explicit prepare retained unsafe assumption", q)
		}
	}
	for _, q := range []string{"CREATE TABLE t(id INT)", "ALTER TABLE t ADD v INT", "SELECT ?", "SHOW TABLES", "-- migration\nCREATE TABLE t(id INT)", "/* ordinary */ SELECT ?"} {
		if !sessionNeutralSQL(q) {
			t.Fatal("unexpected isolation invalidation", q)
		}
	}
}

func TestExecutableCommentsDoNotRetainKnownIsolation(t *testing.T) {
	for _, q := range []string{"/*! SET SESSION transaction_isolation='REPEATABLE-READ' */", "/*+ hint */ SELECT 1", "-- no newline", "/* unterminated"} {
		if sessionNeutralSQL(q) {
			t.Fatal("unknown SQL retained session assumption", q)
		}
	}
}
