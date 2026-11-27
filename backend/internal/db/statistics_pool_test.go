package db_test

import (
	"context"
	"testing"
	"time"
	"yihe-care/internal/db/dbtest"
)

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
