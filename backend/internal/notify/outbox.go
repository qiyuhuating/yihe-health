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

// Probe only after an idle drain. Each EXISTS uses the same indexed status
// and time range as the authoritative claim. A positive result is a hint;
// row locks, leases and commit remain exclusively in Drain.
func (d *Worker) hasReadyMessages(ctx context.Context) (bool, error) {
	now := time.Now()
	var ready bool
	err := d.DB.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM message_outbox WHERE status='sending' AND last_attempt_at<=?) OR
 EXISTS(SELECT 1 FROM message_outbox WHERE status='failed' AND retry_at<=?) OR
 EXISTS(SELECT 1 FROM message_outbox WHERE status='pending' AND retry_at<=?)`,
		now.Add(-time.Minute), now, now).Scan(&ready)
	return ready, err
}

func (d *Worker) drainCycle(ctx context.Context, idle bool) (bool, error) {
	if idle {
		ready, err := d.hasReadyMessages(ctx)
		if err != nil || !ready {
			return err == nil, err
		}
	}
	// Busy bursts go straight through the durable claim path.
	for ctx.Err() == nil {
		processed, err := d.Drain(ctx, 512)
		if err != nil {
			return false, err
		}
		if processed == 0 {
			return true, nil
		}
	}
	return false, nil
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
			idle := false
			for {
				if ctx.Err() != nil {
					return
				}
				cycle, cancel := context.WithTimeout(ctx, 45*time.Second)
				var err error
				idle, err = d.drainCycle(cycle, idle)
				if err != nil && cycle.Err() == nil {
					slog.Warn("message_outbox_cycle_failed", "error", err)
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
