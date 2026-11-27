package db_test

import (
	"context"
	"database/sql"
	"testing"
	"time"
	"yihe-care/internal/db/dbtest"
)

func TestMySQLBusinessPoolBudget(t *testing.T) {
	t.Setenv("MYSQL_MAX_OPEN_CONNS", "4")
	t.Setenv("MYSQL_MAX_IDLE_CONNS", "1")
	t.Setenv("MYSQL_SOS_MAX_CONNS", "1")
	d := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if got := d.DB.Stats().MaxOpenConnections; got != 4 {
		t.Fatalf("business pool maximum = %d, want 4", got)
	}
	conns := make([]*sql.Conn, 0, 4)
	for range 4 {
		conn, err := d.DB.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conns = append(conns, conn)
	}
	wait, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if conn, err := d.DB.Conn(wait); err == nil {
		conn.Close()
		t.Fatal("business pool exceeded configured budget")
	}
	var n int
	if err := d.SOSPool().QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil || n != 1 {
		t.Fatal("SOS pool blocked by saturated business pool", err)
	}
	for _, conn := range conns {
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.DB.Stats().Idle; got != 1 {
		t.Fatalf("idle business connections = %d, want 1", got)
	}
}

func TestMySQLStatisticsPoolIsolation(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "2")
	d := dbtest.MySQL(t)
	if d.StatisticsPool() == d.DB {
		t.Fatal("statistics pool shares business pool")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a, err := d.StatisticsPool().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := d.StatisticsPool().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	wait, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	if c, err := d.StatisticsPool().Conn(wait); err == nil {
		c.Close()
		t.Fatal("statistics pool exceeded budget")
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT 1").Scan(&n); err != nil || n != 1 {
		t.Fatal("business pool blocked by statistics", err)
	}
}

func TestStatisticsPoolRepeatableReadWithoutRedundantSet(t *testing.T) {
	t.Setenv("MYSQL_STATISTICS_MAX_CONNS", "2")
	t.Setenv("MYSQL_STMT_CACHE_SIZE", "32")
	d := dbtest.Fixture(t)
	ctx := t.Context()
	conn, err := d.StatisticsPool().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	options := func() uint64 {
		t.Helper()
		var name string
		var count uint64
		if err := conn.QueryRowContext(ctx, "SHOW SESSION STATUS LIKE 'Com_set_option'").Scan(&name, &count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if _, err := d.ExecContext(ctx, "CREATE TABLE stats_isolation_probe(id INT PRIMARY KEY,v INT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(ctx, "INSERT INTO stats_isolation_probe VALUES(1,1)"); err != nil {
		t.Fatal(err)
	}
	check := func(level sql.IsolationLevel, first, second int, delta uint64) {
		t.Helper()
		before := options()
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: level})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		var v int
		if err = tx.QueryRowContext(ctx, "SELECT v FROM stats_isolation_probe WHERE id=1").Scan(&v); err != nil || v != first {
			t.Fatal(v, err)
		}
		if _, err = d.ExecContext(ctx, "UPDATE stats_isolation_probe SET v=v+1 WHERE id=1"); err != nil {
			t.Fatal(err)
		}
		if err = tx.QueryRowContext(ctx, "SELECT v FROM stats_isolation_probe WHERE id=1").Scan(&v); err != nil || v != second {
			t.Fatal("wrong transaction visibility", v, err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if actual := options() - before; actual != delta {
			t.Fatalf("per-transaction SET count %d, want %d", actual, delta)
		}
	}
	check(sql.LevelRepeatableRead, 1, 1, 0)
	check(sql.LevelReadCommitted, 2, 3, 1)
	check(sql.LevelRepeatableRead, 3, 3, 0)
	if _, err = conn.ExecContext(ctx, "SET SESSION transaction_isolation='READ-COMMITTED'"); err != nil {
		t.Fatal(err)
	}
	check(sql.LevelRepeatableRead, 4, 4, 1)
}
