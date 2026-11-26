package db_test

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"yihe-care/internal/db"
)

// The server withdraws all business routes when this probe fails. A busy
// business pool must not be reported as a disconnected primary database.
func TestMySQLPingDuringPoolSaturation(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	pool.DB.SetMaxOpenConns(1)
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if pool.DB.Stats().InUse != 1 {
		t.Fatal("business pool is not saturated")
	}
	probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
	defer stop()
	if err := pool.PingContext(probe); err != nil {
		t.Fatalf("healthy MySQL withdrawn under pool saturation: %v", err)
	}
}

func TestMySQLPingReportsConnectionLoss(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" {
		t.Skip("requires TCP TEST_MYSQL_DSN")
	}
	var down atomic.Bool
	var mu sync.Mutex
	var connections []net.Conn
	network := "probe-" + uuid.NewString()
	mysql.RegisterDialContext(network, func(ctx context.Context, address string) (net.Conn, error) {
		if down.Load() {
			return nil, errors.New("test database disconnected")
		}
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
		if err == nil {
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
		}
		return conn, err
	})
	defer mysql.DeregisterDialContext(network)
	cfg.Net = network
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := pool.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	down.Store(true)
	mu.Lock()
	for _, conn := range connections {
		conn.Close()
	}
	mu.Unlock()
	if err := pool.PingContext(ctx); err == nil {
		t.Fatal("readiness concealed a disconnected MySQL server")
	}
	down.Store(false)
	if err := pool.PingContext(ctx); err != nil {
		t.Fatalf("readiness did not recover after reconnection: %v", err)
	}
}
