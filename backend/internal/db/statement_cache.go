package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// This wrapper depends on the pinned mysql driver's context, validation and
// value conversion interfaces. Embed the whole contract so pool liveness,
// transaction isolation and cancellation cannot silently fall back to legacy
// driver methods. database/sql serializes operations on each physical Conn.
type mysqlWireConn interface {
	driver.Conn
	driver.ConnPrepareContext
	driver.ConnBeginTx
	driver.ExecerContext
	driver.QueryerContext
	driver.NamedValueChecker
	driver.Pinger
	driver.SessionResetter
	driver.Validator
}
type mysqlWireStmt interface {
	driver.Stmt
	driver.StmtExecContext
	driver.StmtQueryContext
}
type statementConnector struct {
	driver.Connector
	capacity             int
	readCommittedSession bool
}

func (c statementConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	wire, ok := conn.(mysqlWireConn)
	if !ok {
		conn.Close()
		return nil, errors.New("MySQL driver lacks required connection interfaces")
	}
	return &statementConn{mysqlWireConn: wire, plans: make(map[string]mysqlWireStmt, c.capacity), keys: make([]string, c.capacity), readCommittedSession: c.readCommittedSession}, nil
}

type statementConn struct {
	mysqlWireConn
	plans                map[string]mysqlWireStmt
	keys                 []string
	next                 int
	readCommittedSession bool
}

// The connector initializes the session to READ COMMITTED. Ordinary application
// DML/DDL cannot change that default, so avoid mysql's per-transaction SET round
// trip. Unknown/session-changing SQL permanently disables this shortcut on that
// connection; the explicit isolation request then follows the original driver.
func sessionNeutralSQL(query string) bool {
	q := strings.TrimSpace(query)
	// Migration templates start with ordinary comments. Skip only comments
	// that cannot execute SQL; MySQL version comments/hints stay conservative.
	for len(q) > 0 {
		if q[0] == '#' || (strings.HasPrefix(q, "--") && (len(q) == 2 || q[2] <= 32)) {
			end := strings.IndexByte(q, '\n')
			if end < 0 {
				return false
			}
			q = strings.TrimSpace(q[end+1:])
			continue
		}
		if strings.HasPrefix(q, "/*") && len(q) > 2 && q[2] != '!' && q[2] != '+' {
			end := strings.Index(q[2:], "*/")
			if end < 0 {
				return false
			}
			q = strings.TrimSpace(q[end+4:])
			continue
		}
		break
	}
	for _, prefix := range []string{"SELECT ", "INSERT ", "UPDATE ", "DELETE ", "CREATE ", "ALTER ", "DROP ", "TRUNCATE ", "SHOW ", "EXPLAIN ", "DESCRIBE "} {
		if len(q) >= len(prefix) && strings.EqualFold(q[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}
func (c *statementConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.readCommittedSession && opts.Isolation == driver.IsolationLevel(sql.LevelReadCommitted) {
		opts.Isolation = driver.IsolationLevel(sql.LevelDefault)
	}
	return c.mysqlWireConn.BeginTx(ctx, opts)
}
func (c *statementConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if !sessionNeutralSQL(query) {
		c.readCommittedSession = false
	}
	return c.mysqlWireConn.PrepareContext(ctx, query)
}
func (c *statementConn) Prepare(query string) (driver.Stmt, error) {
	if !sessionNeutralSQL(query) {
		c.readCommittedSession = false
	}
	return c.mysqlWireConn.Prepare(query)
}

// Entries belong exclusively to this physical connection. No global cache lock,
// extra pool checkout, transaction escape or user values in cache keys. Close of
// a connection frees its server statements; eviction closes exactly one handle.
func (c *statementConn) statement(ctx context.Context, query string, count int) (mysqlWireStmt, error) {
	stmt := c.plans[query]
	if stmt == nil {
		// Free capacity before preparing: a full server-side statement budget
		// must not prevent replacement, even transiently on a cache miss.
		if old := c.plans[c.keys[c.next]]; old != nil {
			delete(c.plans, c.keys[c.next])
			if err := old.Close(); err != nil {
				return nil, err
			}
		}
		prepared, err := c.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		var ok bool
		stmt, ok = prepared.(mysqlWireStmt)
		if !ok {
			prepared.Close()
			return nil, errors.New("MySQL driver lacks context statement interfaces")
		}
		c.plans[query] = stmt
		c.keys[c.next] = query
		c.next = (c.next + 1) % len(c.keys)
	}
	// ExecerContext bypasses database/sql's ordinary statement argument check.
	// Reject mismatches before sending any execute packet.
	if stmt.NumInput() != count {
		return nil, fmt.Errorf("sql: expected %d arguments, got %d", stmt.NumInput(), count)
	}
	return stmt, nil
}
func unsupportedPrepare(err error) bool {
	var my *mysql.MySQLError
	return errors.As(err, &my) && my.Number == 1295
}

// Only cache ordinary DML. Session-changing and DDL calls are forwarded after
// clearing handles, so SET sql_mode / USE cannot leave old parsing semantics.
// A leading comment is conservatively uncacheable rather than implementing a
// second SQL lexer. These are fixed application templates, not user input.
func cacheableDML(query string) bool {
	query = strings.TrimSpace(query)
	for _, prefix := range []string{"SELECT ", "INSERT ", "UPDATE ", "DELETE "} {
		if len(query) >= len(prefix) && strings.EqualFold(query[:len(prefix)], prefix) {
			return true
		}
	}
	return false
}
func (c *statementConn) clearStatements() error {
	var first error
	for query, stmt := range c.plans {
		if err := stmt.Close(); first == nil {
			first = err
		}
		delete(c.plans, query)
	}
	clear(c.keys)
	c.next = 0
	return first
}
func (c *statementConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !sessionNeutralSQL(query) {
		c.readCommittedSession = false
	}
	if len(args) == 0 || !cacheableDML(query) {
		if err := c.clearStatements(); err != nil {
			return nil, err
		}
		return c.mysqlWireConn.ExecContext(ctx, query, args)
	}
	stmt, err := c.statement(ctx, query, len(args))
	if unsupportedPrepare(err) {
		return c.mysqlWireConn.ExecContext(ctx, query, args)
	}
	if err != nil {
		return nil, err
	}
	return stmt.ExecContext(ctx, args)
}
func (c *statementConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !sessionNeutralSQL(query) {
		c.readCommittedSession = false
	}
	if !cacheableDML(query) {
		q := strings.TrimSpace(query)
		if len(q) < 5 || !strings.EqualFold(q[:5], "SHOW ") {
			if err := c.clearStatements(); err != nil {
				return nil, err
			}
		}
		return c.mysqlWireConn.QueryContext(ctx, query, args)
	}
	if len(args) == 0 {
		return c.mysqlWireConn.QueryContext(ctx, query, args)
	}
	stmt, err := c.statement(ctx, query, len(args))
	if unsupportedPrepare(err) {
		return c.mysqlWireConn.QueryContext(ctx, query, args)
	}
	if err != nil {
		return nil, err
	}
	return stmt.QueryContext(ctx, args)
}
func (c *statementConn) Close() error {
	// COM_QUIT frees all server-side handles in one operation. The private Stmts
	// never escape the wrapper, so no individual COM_STMT_CLOSE packets are needed.
	c.plans = nil
	c.keys = nil
	return c.mysqlWireConn.Close()
}
func mysqlStatementCacheLimit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 64, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 || n > 128 {
		return 0, errors.New("MYSQL_STMT_CACHE_SIZE must be an integer from 0 to 128")
	}
	return n, nil
}
