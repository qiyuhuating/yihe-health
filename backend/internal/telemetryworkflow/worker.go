package telemetryworkflow

import (
	"context"
	"database/sql"
	"log"
	"time"

	"yihe-care/internal/db"
	"yihe-care/internal/observability"
	"yihe-care/internal/telemetryrepo"
)

const batchSize = 32

type Process func(context.Context, *sql.Tx, telemetryrepo.DurableItem) error

// Drain owns global ordering, per-item savepoints, retry state and atomic commit.
func Drain(ctx context.Context, store *db.Database, process Process) (int, error) {
	started := time.Now()
	defer func() {
		observability.Default.Add("yihe_telemetry_drain_seconds_total", time.Since(started).Seconds())
	}()
	tx, err := store.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	items, err := telemetryrepo.LockDurableBatch(ctx, tx, batchSize)
	if err != nil || len(items) == 0 {
		return 0, err
	}
	now := time.Now().UTC()
	processed := 0
	for _, item := range items {
		// A failed older item may not be overtaken by later daily updates.
		if item.NextRetryAt.After(now) {
			break
		}
		processed++
		if _, err := tx.ExecContext(ctx, "SAVEPOINT telemetry_inbox_item"); err != nil {
			return 0, err
		}
		if err := process(ctx, tx, item); err != nil {
			if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT telemetry_inbox_item"); rollbackErr != nil {
				return 0, rollbackErr
			}
			if err := telemetryrepo.MarkDurableFailure(ctx, tx, item, err.Error()); err != nil {
				return 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT telemetry_inbox_item"); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	observability.Default.Add("yihe_telemetry_drain_processed_total", float64(processed))
	return processed, nil
}

func Run(ctx context.Context, store *db.Database, process Process) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		run, cancel := context.WithTimeout(ctx, 10*time.Second)
		count, err := Drain(run, store, process)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("durable telemetry batch failed; pending rows retained: %v", err)
		}
		if count > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
