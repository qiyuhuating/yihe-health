package db_test

import (
	"database/sql"
	"testing"

	"yihe-care/internal/db/dbtest"
)

func TestMySQLTelemetrySavepointRetainsPreparedHandles(t *testing.T) {
	t.Setenv("MYSQL_STMT_CACHE_SIZE", "64")
	p := dbtest.MySQL(t)
	ctx := t.Context()
	conn, err := p.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "CREATE TEMPORARY TABLE savepoint_cache_probe(id BIGINT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	counter := func(name string) int64 {
		t.Helper()
		var key string
		var count int64
		if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE '"+name+"'").Scan(&key, &count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	prepareBefore := counter("Com_stmt_prepare")
	closeBefore := counter("Com_stmt_close")
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 1; i <= 32; i++ {
		if _, err = tx.ExecContext(ctx, "SAVEPOINT telemetry_inbox_item"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO savepoint_cache_probe VALUES(?)", i); err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			if _, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT telemetry_inbox_item"); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = tx.ExecContext(ctx, "RELEASE SAVEPOINT telemetry_inbox_item"); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	prepared := counter("Com_stmt_prepare") - prepareBefore
	closed := counter("Com_stmt_close") - closeBefore
	t.Logf("32 inserts across savepoints: prepare=%d close=%d", prepared, closed)
	if prepared != 1 || closed != 0 {
		t.Fatalf("repeated statement was not retained: prepare=%d close=%d", prepared, closed)
	}
	var rows int
	if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM savepoint_cache_probe").Scan(&rows); err != nil || rows != 16 {
		t.Fatalf("savepoint rollback changed: rows=%d err=%v", rows, err)
	}
}
