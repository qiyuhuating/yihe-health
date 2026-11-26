package db_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/db/dbtest"
)

func TestMySQLParameterTransportRoundTrips(t *testing.T) {
	primary := dbtest.MySQL(t)
	ctx := context.Background()
	var schema string
	if err := primary.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	base, err := mysql.ParseDSN(os.Getenv("TEST_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	base.DBName = schema
	base.ParseTime = true
	base.Loc = time.UTC
	for _, interpolate := range []bool{false, true} {
		cfg := *base
		cfg.InterpolateParams = interpolate
		cfg.Params = map[string]string{"charset": "utf8mb4"}
		pool, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
		conn, err := pool.Conn(ctx)
		if err != nil {
			pool.Close()
			t.Fatal(err)
		}
		for _, mode := range []string{"", "NO_BACKSLASH_ESCAPES"} {
			if _, err := conn.ExecContext(ctx, "SET SESSION sql_mode=?", mode); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, "DROP TEMPORARY TABLE IF EXISTS transport_probe"); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(ctx, "CREATE TEMPORARY TABLE transport_probe(j JSON,b LONGBLOB,s LONGTEXT,sample_time DATETIME(6))"); err != nil {
				t.Fatal(err)
			}
			prepares := func() uint64 {
				var name string
				var count uint64
				if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_stmt_prepare'").Scan(&name, &count); err != nil {
					t.Fatal(err)
				}
				return count
			}
			before := prepares()
			text := "中文 ☁️ ' \\ ; ? \x00\n"
			raw, _ := json.Marshal(map[string]string{"text": text})
			binary := []byte{0, 255, 39, 92, 63, 0, 128}
			at := time.Date(2026, 10, 23, 1, 2, 3, 123456000, time.UTC)
			if _, err := conn.ExecContext(ctx, "INSERT INTO transport_probe VALUES(?,?,?,?)", json.RawMessage(raw), binary, text, at); err != nil {
				t.Fatal(err)
			}
			var gotJSON, gotText string
			var gotBinary []byte
			var gotTime time.Time
			if err := conn.QueryRowContext(ctx, "SELECT JSON_UNQUOTE(JSON_EXTRACT(j,'$.text')),b,s,sample_time FROM transport_probe").Scan(&gotJSON, &gotBinary, &gotText, &gotTime); err != nil {
				t.Fatal(err)
			}
			if gotJSON != text || gotText != text || !bytes.Equal(gotBinary, binary) || !gotTime.Equal(at) {
				t.Fatal("JSON/binary/text/time transport changed")
			}
			after := prepares()
			if interpolate && after != before {
				t.Fatalf("unexpected prepare: %d -> %d", before, after)
			}
			if !interpolate && after <= before {
				t.Fatal("binary prepare control did not execute")
			}
			// A quoted question mark must remain literal in both transport modes.
			var marker, returned string
			if err := conn.QueryRowContext(ctx, "SELECT '?' AS marker, ? AS value", text).Scan(&marker, &returned); err != nil || marker != "?" || returned != text {
				t.Fatal("quoted marker changed bound values", err)
			}
			literalAfter := prepares()
			if interpolate && literalAfter != after {
				t.Fatalf("quoted marker triggered an unexpected prepare: %d -> %d", after, literalAfter)
			}
			if !interpolate && literalAfter <= after {
				t.Fatal("quoted marker did not use binary prepare with interpolation disabled")
			}
			var nilBytes, nilValue bool
			if err := conn.QueryRowContext(ctx, "SELECT ? IS NULL, ? IS NULL", []byte(nil), nil).Scan(&nilBytes, &nilValue); err != nil || !nilBytes || !nilValue {
				t.Fatal("SQL NULL transport changed", err)
			}
		}
		conn.Close()
		pool.Close()
	}
}

// Exercise OpenMySQL's actual connector on one physical connection, including
// transactions. Counts are session-local and cannot be polluted by other tests.
func TestMySQLConnectionStatementCacheLifecycle(t *testing.T) {
	t.Setenv("MYSQL_STMT_CACHE_SIZE", "2")
	primary := dbtest.MySQL(t)
	ctx := context.Background()
	conn, err := primary.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "CREATE TEMPORARY TABLE cache_probe(id INT PRIMARY KEY,j JSON,b BLOB,s TEXT,at DATETIME(6))"); err != nil {
		t.Fatal(err)
	}
	prepares := func() uint64 {
		var name string
		var count uint64
		if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_stmt_prepare'").Scan(&name, &count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := prepares()
	text := "中文 '\\ ? \x00"
	raw, _ := json.Marshal(map[string]string{"text": text})
	blob := []byte{0, 255, 128, 92}
	at := time.Date(2026, 10, 23, 1, 2, 3, 123456000, time.UTC)
	insert := "INSERT INTO cache_probe VALUES(?,?,?,?,?)"
	selectRow := "SELECT JSON_UNQUOTE(JSON_EXTRACT(j,'$.text')),b,s,at FROM cache_probe WHERE id=?"
	for id := 1; id <= 3; id++ {
		if _, err = conn.ExecContext(ctx, insert, id, json.RawMessage(raw), blob, text, at); err != nil {
			t.Fatal(err)
		}
		var gotText, gotJSON string
		var gotBlob []byte
		var gotAt time.Time
		if err = conn.QueryRowContext(ctx, selectRow, id).Scan(&gotJSON, &gotBlob, &gotText, &gotAt); err != nil {
			t.Fatal(err)
		}
		if gotJSON != text || gotText != text || !bytes.Equal(gotBlob, blob) || !gotAt.Equal(at) {
			t.Fatal("cached binary transport changed values")
		}
	}
	if after := prepares(); after-before != 2 {
		t.Fatal("did not reuse both prepared handles", before, after)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, insert, 99, json.RawMessage(raw), blob, text, at); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM cache_probe WHERE id=99").Scan(&count); err != nil || count != 0 {
		t.Fatal("statement escaped rollback", count, err)
	}
	if prepares()-before != 2 {
		t.Fatal("transaction failed to share physical-connection handle")
	}
	// SET with parameters must flush old parse state, then use driver fallback.
	if _, err = conn.ExecContext(ctx, "SET SESSION sql_mode=?", "NO_BACKSLASH_ESCAPES"); err != nil {
		t.Fatal(err)
	}
	var returned string
	if err = conn.QueryRowContext(ctx, "SELECT ?", text).Scan(&returned); err != nil || returned != text {
		t.Fatal("mode change corrupted parameters", returned, err)
	}
	if err = conn.QueryRowContext(ctx, "SELECT ?+1", 1).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err = conn.QueryRowContext(ctx, "SELECT ?+2", 1).Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	after := prepares()
	if err = conn.QueryRowContext(ctx, "SELECT ?", text).Scan(&returned); err != nil || returned != text || prepares() != after+1 {
		t.Fatal("bounded FIFO did not evict/reprepare", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = conn.ExecContext(cancelled, insert, 100, json.RawMessage(raw), blob, text, at); err == nil {
		t.Fatal("cancelled write executed")
	}
	if err = conn.QueryRowContext(ctx, "SELECT count(*) FROM cache_probe WHERE id=100").Scan(&count); err != nil || count != 0 {
		t.Fatal("cancelled write persisted", count, err)
	}
}

func TestMySQLSessionIsolationShortcutAndMutationFallback(t *testing.T) {
	primary := dbtest.MySQL(t)
	ctx := context.Background()
	conn, err := primary.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// The migrated connection keeps the configured session default. Each normal
	// BeginTx must no longer send SET TRANSACTION before START TRANSACTION.
	var isolation string
	if err = conn.QueryRowContext(ctx, "SELECT @@session.transaction_isolation").Scan(&isolation); err != nil || isolation != "READ-COMMITTED" {
		t.Fatal(isolation, err)
	}
	options := func() uint64 {
		var name string
		var count uint64
		if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_set_option'").Scan(&name, &count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	before := options()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if after := options(); after != before {
		t.Fatal("redundant per-transaction SET remains", before, after)
	}
	// An explicit session change invalidates the shortcut. Verify actual visibility
	// with a second physical connection, rather than querying only the session
	// default (which does not report a transaction-specific isolation override).
	if _, err = conn.ExecContext(ctx, "CREATE TABLE isolation_probe(id INT PRIMARY KEY,v INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, "INSERT INTO isolation_probe VALUES(1,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, "SET SESSION transaction_isolation='REPEATABLE-READ'"); err != nil {
		t.Fatal(err)
	}
	before = options()
	tx, err = conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var value int
	if err = tx.QueryRowContext(ctx, "SELECT v FROM isolation_probe WHERE id=1").Scan(&value); err != nil || value != 1 {
		t.Fatal(value, err)
	}
	if _, err = primary.DB.ExecContext(ctx, "UPDATE isolation_probe SET v=2 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRowContext(ctx, "SELECT v FROM isolation_probe WHERE id=1").Scan(&value); err != nil || value != 2 {
		t.Fatal("READ COMMITTED visibility lost after session mutation", value, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if after := options(); after != before+1 {
		t.Fatal("did not restore explicit SET TRANSACTION", before, after)
	}
}
