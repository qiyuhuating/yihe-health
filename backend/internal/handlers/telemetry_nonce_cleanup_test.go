package handlers

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

type nonceCleanupResult int64

func (r nonceCleanupResult) LastInsertId() (int64, error) { return 0, nil }
func (r nonceCleanupResult) RowsAffected() (int64, error) { return int64(r), nil }

func TestSignedNonceCleanupUsesBoundedBatches(t *testing.T) {
	cutoff := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
	attempts := 0
	err := cleanupExpiredSignedDeviceNonces(context.Background(), cutoff, func(ctx context.Context, query string, args ...any) (sql.Result, error) {
		attempts++
		if query != "DELETE FROM device_request_nonces WHERE expires_at < ? LIMIT 5000" {
			t.Fatalf("query: %s", query)
		}
		if len(args) != 1 || args[0].(time.Time) != cutoff {
			t.Fatal("cutoff changed across batches")
		}
		if attempts < 3 {
			return nonceCleanupResult(5000), nil
		}
		return nonceCleanupResult(12), nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("batches=%d err=%v", attempts, err)
	}
	attempts = 0
	err = cleanupExpiredSignedDeviceNonces(context.Background(), cutoff, func(ctx context.Context, _ string, _ ...any) (sql.Result, error) {
		attempts++
		return nonceCleanupResult(5000), nil
	})
	if err != nil || attempts != 4 {
		t.Fatalf("unbounded cleanup: batches=%d err=%v", attempts, err)
	}
}

func TestSignedNonceCleanupStopsOnError(t *testing.T) {
	marker := errors.New("database unavailable")
	calls := 0
	err := cleanupExpiredSignedDeviceNonces(context.Background(), time.Now(), func(context.Context, string, ...any) (sql.Result, error) { calls++; return nil, marker })
	if !errors.Is(err, marker) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
