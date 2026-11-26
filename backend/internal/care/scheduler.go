package care

import (
	"context"
	"log/slog"
	"strconv"
	"yihe-care/internal/config"
	"sync"
	"time"
)

func deliveryConcurrency() int {
	n, err := strconv.Atoi(config.Setting("CARE_NOTIFY_CONCURRENCY"))
	if err != nil || n < 1 || n > 64 {
		return 8
	}
	return n
}

// External I/O has its own bounded workers. A slow provider cannot hold up
// the maintenance loop's escalation, heartbeat or offline scan.
func (d *dispatcher) runDispatchers(ctx context.Context, concurrency int) {
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			for ctx.Err() == nil {
				attempt, cancel := context.WithTimeout(ctx, 20*time.Second)
				processed, err := d.dispatchNext(attempt)
				cancel()
				if err != nil && ctx.Err() == nil {
					slog.Warn("care_delivery_attempt_failed")
				}
				if processed && err == nil {
					continue
				}
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		})
	}
	workers.Wait()
}
