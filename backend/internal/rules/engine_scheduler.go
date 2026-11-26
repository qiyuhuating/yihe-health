package rules

import (
	"context"
	"log/slog"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
	"sync"
	"time"
)

func (e *Engine) Tick(ctx context.Context) error {
	ids, err := db.QueryList(ctx, e.DB, db.ScanValue[int64], "SELECT device_id FROM device WHERE device_id>? AND old_id IS NOT NULL AND status<>0 ORDER BY device_id LIMIT 100", e.After)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		e.After = 0
		return nil
	}
	// Each device retains its own transaction and row lock. Bound concurrency to
	// leave connections for ingestion and notification delivery.
	errors := make([]error, len(ids))
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	limit := e.Workers
	if limit < 1 || limit > 8 {
		limit = 8
	}
	for range min(limit, len(ids)) {
		workers.Go(func() {
			for i := range jobs {
				errors[i] = e.Device(ctx, ids[i], time.Now().UTC())
			}
		})
	}
	workers.Wait()
	for i, id := range ids {
		if errors[i] != nil {
			return errors[i]
		}
		e.After = id
	}
	return nil
}

func Run(ctx context.Context, pool *db.Database, box *securestore.Box) {
	e := Engine{DB: pool, Box: box, Workers: deviceWorkers(config.Setting("RULE_DEVICE_WORKERS"))}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		cycle, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := e.Tick(cycle)
		if err != nil && cycle.Err() == nil {
			slog.Warn("rule_engine_cycle_failed")
		}
		cancel()
		// Drain the remaining pages immediately; waiting between every 100
		// devices otherwise caps a healthy deployment at 20 evaluations/second.
		if err == nil && e.After != 0 && ctx.Err() == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
