package db_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"yihe-care/internal/db/dbtest"
)

type idlePoolMeasurement struct {
	IdleLimit        int     `json:"idle_limit"`
	Trial            int     `json:"trial"`
	Requests         int     `json:"requests"`
	Concurrency      int     `json:"concurrency"`
	AfterWarmupP50MS float64 `json:"after_warmup_p50_ms"`
	AfterWarmupP95MS float64 `json:"after_warmup_p95_ms"`
	AfterWarmupP99MS float64 `json:"after_warmup_p99_ms"`
	MaxIdleClosed    int64   `json:"max_idle_closed_delta"`
	PoolWaitMS       float64 `json:"pool_wait_ms"`
	IdleConnections  int     `json:"idle_connections_after"`
	OpenConnections  int     `json:"open_connections_after"`
}

// An opt-in experiment on a disposable MySQL database. Each trial begins with
// zero idle connections; all modes share MaxOpenConns=20. A cold burst is
// excluded from the measured distribution, while 8 subsequent 32-way bursts
// exercise the retention limit under short pauses typical of device uploads.
func TestMySQLIdleConnectionRetentionAB(t *testing.T) {
	if os.Getenv("RUN_MYSQL_POOL_AB") != "1" {
		t.Skip("set RUN_MYSQL_POOL_AB=1 for isolated pool A/B")
	}
	p := dbtest.MySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	sqlDB := p.DB
	const concurrency = 32
	const measuredBursts = 8
	for trial, idleLimit := range []int{5, 20, 10, 10, 20, 5} {
		sqlDB.SetMaxIdleConns(0)
		sqlDB.SetMaxIdleConns(idleLimit)
		if sqlDB.Stats().Idle != 0 {
			t.Fatal("trial unexpectedly retained connections")
		}
		before := sqlDB.Stats()
		latencies := make([]time.Duration, 0, measuredBursts*concurrency)
		for burst := 0; burst <= measuredBursts; burst++ {
			ready := make(chan struct{})
			samples := make(chan time.Duration, concurrency)
			failures := make(chan error, concurrency)
			var done sync.WaitGroup
			for i := 0; i < concurrency; i++ {
				done.Add(1)
				go func() {
					defer done.Done()
					<-ready
					start := time.Now()
					var revision int64
					err := p.QueryRowContext(ctx, "SELECT revision FROM health_clock WHERE id=?", 1).Scan(&revision)
					if err != nil {
						failures <- err
						return
					}
					if revision != 0 {
						failures <- fmt.Errorf("unexpected health_clock revision %d", revision)
						return
					}
					samples <- time.Since(start)
				}()
			}
			close(ready)
			done.Wait()
			close(samples)
			close(failures)
			for err := range failures {
				t.Fatal(err)
			}
			for latency := range samples {
				if burst > 0 {
					latencies = append(latencies, latency)
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		if len(latencies) != measuredBursts*concurrency {
			t.Fatalf("retention %d: got %d samples", idleLimit, len(latencies))
		}
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		percentile := func(n int) float64 {
			i := (len(latencies)*n+99)/100 - 1
			if i < 0 {
				i = 0
			}
			return float64(latencies[i].Microseconds()) / 1000
		}
		after := sqlDB.Stats()
		output, _ := json.Marshal(idlePoolMeasurement{
			IdleLimit: idleLimit, Trial: trial, Requests: len(latencies),
			Concurrency:      concurrency,
			AfterWarmupP50MS: percentile(50),
			AfterWarmupP95MS: percentile(95),
			AfterWarmupP99MS: percentile(99),
			MaxIdleClosed:    after.MaxIdleClosed - before.MaxIdleClosed,
			PoolWaitMS:       float64((after.WaitDuration - before.WaitDuration).Microseconds()) / 1000,
			IdleConnections:  after.Idle, OpenConnections: after.OpenConnections,
		})
		t.Logf("MYSQL_IDLE_POOL_AB %s", output)
	}
}
