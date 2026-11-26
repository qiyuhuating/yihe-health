package db_test

import (
	"context"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

// A busy request pool must not cause the liveness probe to report an outage.
// This is a MySQL integration test; it skips unless TEST_MYSQL_DSN is set.
func TestLivenessProbeDoesNotWaitForBusyBusinessPool(t *testing.T) {
	pool := dbtest.MySQL(t)
	pool.DB.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := pool.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// With the old implementation, Ping blocked on the held business pool
	// connection and failed at the deadline. The reserved probe should succeed.
	if err = pool.PingContext(ctx); err != nil {
		t.Fatalf("health probe starved by busy SQL pool: %v", err)
	}
}
