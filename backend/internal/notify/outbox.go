package notify

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/securestore"
)

type Worker struct {
	DB         *db.Database
	Box        *securestore.Box
	URL, Token string
	BulkURL    string
	Client     *http.Client
}

// Tick uses a persisted lease token: an expired worker cannot overwrite a new claim.
func (d *Worker) Tick(ctx context.Context) error {
	_, err := d.tick(ctx)
	return err
}

func (d *Worker) tick(ctx context.Context) (bool, error) {
	n, err := d.Drain(ctx, 1)
	return n > 0, err
}

// Keep workers below the 20-connection DB pool ceiling. Multiple workers can
// drain separate SKIP LOCKED batches while slow HTTP requests are in flight.
// The default preserves the existing single-worker deployment behavior.
func drainConcurrency() int {
	n, err := strconv.Atoi(strings.TrimSpace(config.Setting("NOTIFY_DRAIN_CONCURRENCY")))
	if err != nil || n < 1 || n > 8 {
		return 1
	}
	return n
}

func Run(ctx context.Context, pool *db.Database, box *securestore.Box) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 64
	transport.MaxIdleConnsPerHost = 64
	transport.MaxConnsPerHost = 64
	transport.IdleConnTimeout = 90 * time.Second
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 10 * time.Second
	defer transport.CloseIdleConnections()
	d := Worker{DB: pool, Box: box, URL: strings.TrimSpace(config.Setting("NOTIFY_WEBHOOK_URL")), BulkURL: strings.TrimSpace(config.Setting("NOTIFY_BULK_WEBHOOK_URL")), Token: config.Secret("NOTIFY_WEBHOOK_TOKEN"), Client: &http.Client{Timeout: 10 * time.Second, Transport: transport}}
	var workers sync.WaitGroup
	for range drainConcurrency() {
		workers.Go(func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if ctx.Err() != nil {
					return
				}
				cycle, cancel := context.WithTimeout(ctx, 45*time.Second)
				// Drain a bounded ready burst; the timer is idle polling, not a per-job delay.
				for cycle.Err() == nil {
					processed, err := d.Drain(cycle, 512)
					if err != nil {
						if cycle.Err() == nil {
							slog.Warn("message_outbox_cycle_failed", "error", err)
						}
						break
					}
					if processed == 0 {
						break
					}
				}
				cancel()
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		})
	}
	workers.Wait()
}
