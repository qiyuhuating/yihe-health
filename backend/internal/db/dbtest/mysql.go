// Package dbtest creates disposable databases for MySQL integration tests.
package dbtest

import (
	"context"
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"yihe-care/internal/db"
)

func MySQL(t testing.TB) *db.Database {
	t.Helper()
	pool := emptyMySQL(t, false)
	if err := db.MigrateMySQL(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func emptyMySQL(t testing.TB, snapshot bool) *db.Database {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, releaseAdmin, err := fixtureAdmin(cfg.FormatDSN(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	name := "yihe_test_" + uuid.NewString()[:8]
	if _, err = admin.ExecContext(context.Background(), "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		releaseAdmin()
		t.Fatal(err)
	}
	var pool *db.Database
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		_, err := admin.ExecContext(context.Background(), "DROP DATABASE `"+name+"`")
		releaseAdmin()
		if err != nil {
			t.Error(err)
		}
	})
	cfg.DBName = name
	pool, err = db.OpenMySQL(context.Background(), cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

// Only schema creation/deletion uses this pool. Business connections always
// belong to their individual fixture. Keep at most one warm admin connection
// per test process; a different DSN gets an independently closed pool.
var snapshotAdmin struct {
	sync.Mutex
	dsn  string
	pool *sql.DB
}

func fixtureAdmin(dsn string, reuse bool) (*sql.DB, func(), error) {
	if reuse {
		snapshotAdmin.Lock()
		defer snapshotAdmin.Unlock()
		if snapshotAdmin.pool == nil {
			pool, err := sql.Open("mysql", dsn)
			if err != nil {
				return nil, nil, err
			}
			pool.SetMaxOpenConns(2)
			pool.SetMaxIdleConns(1)
			pool.SetConnMaxIdleTime(30 * time.Second)
			pool.SetConnMaxLifetime(2 * time.Minute)
			snapshotAdmin.dsn, snapshotAdmin.pool = dsn, pool
		}
		if snapshotAdmin.dsn == dsn {
			return snapshotAdmin.pool, func() {}, nil
		}
	}
	pool, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, nil, err
	}
	return pool, func() { _ = pool.Close() }, nil
}
