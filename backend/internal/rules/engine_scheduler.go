package rules

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

type dueRuleTask struct {
	id  int64
	due time.Time
}

// Tick visits only the due index, never the device inventory. A fixed cutoff
// and keyset cursor allow failed tasks to stay durable without starving later
// tasks. A subsequent tick retries failures; there is no in-memory claim to lose.
func (e *Engine) Tick(ctx context.Context) error {
	cutoff := time.Now().UTC()
	var after time.Time
	var afterID int64
	var firstErr error
	limit := e.Workers
	if limit < 1 || limit > 8 {
		limit = 8
	}
	for {
		query := `SELECT device_id,due_at FROM rule_task WHERE due_at<=?`
		args := []any{cutoff}
		if !after.IsZero() {
			query += ` AND (due_at>? OR (due_at=? AND device_id>?))`
			args = append(args, after, after, afterID)
		}
		query += ` ORDER BY due_at,device_id LIMIT 100`
		tasks, err := db.QueryList(ctx, e.DB, func(rows *sql.Rows) (dueRuleTask, error) {
			var task dueRuleTask
			err := rows.Scan(&task.id, &task.due)
			return task, err
		}, query, args...)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			return firstErr
		}
		failures := make([]error, len(tasks))
		jobs := make(chan int, len(tasks))
		for i := range tasks {
			jobs <- i
		}
		close(jobs)
		var workers sync.WaitGroup
		for range min(limit, len(tasks)) {
			workers.Go(func() {
				for i := range jobs {
					task := tasks[i]
					// Once overdue by one full tick, wait for the row under the
					// cycle deadline instead of starving a continuously busy device.
					failures[i] = e.device(ctx, task.id, time.Now().UTC(), &cutoff, cutoff.Sub(task.due) < 5*time.Second)
				}
			})
		}
		workers.Wait()
		for _, err := range failures {
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		last := tasks[len(tasks)-1]
		after, afterID = last.due, last.id
		if len(tasks) < 100 {
			return firstErr
		}
	}
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
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
