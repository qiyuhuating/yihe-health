package handlers

import (
	"context"
	"database/sql"
	"slices"
	"sync"
	"time"
)

// Fixed-rate producer independent of service completion. Each domain has
// one consumer, two waiting slots, and no unbounded goroutine accumulation.
func mixedFixedArrivals(ctx context.Context, start time.Time, period time.Duration, n int, run func(int, time.Time)) (offered, dropped int) {
	type arrival struct {
		seq int
		at  time.Time
	}
	queue := make(chan arrival, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		for job := range queue {
			run(job.seq, job.at)
		}
	})
	defer func() { close(queue); wg.Wait() }()
	for i := 1; i <= n; i++ {
		at := start.Add(time.Duration(i-1) * period)
		timer := time.NewTimer(time.Until(at))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		offered++
		select {
		case queue <- arrival{i, at}:
		default:
			dropped++
		}
	}
	return
}

type mixedCommitController struct {
	admission        *healthWriteAdmission
	enabled          bool
	mu               sync.Mutex
	commit, business []time.Duration
	changes          []mixedFeedbackChange
}

type mixedFeedbackChange struct {
	Limit                             int
	CommitP90MS, SQLP90MS, SyncWaitMS float64
}

func (c *mixedCommitController) observe(stage string, d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch stage {
	case "commit":
		c.commit = append(c.commit, d)
	case "business_sql":
		c.business = append(c.business, d)
	}
}

func (c *mixedCommitController) sample() (commit, sql time.Duration, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.commit) >= 12 && len(c.business) >= 12 {
		slices.Sort(c.commit)
		slices.Sort(c.business)
		commit = c.commit[(len(c.commit)*9+9)/10-1]
		sql = c.business[(len(c.business)*9+9)/10-1]
		ok = true
	}
	c.commit = c.commit[:0]
	c.business = c.business[:0]
	return
}

// The controller is an experiment, not a production adaptive limiter.
// Read the same cumulative file wait counters in both arms. Decrease
// admission only before BEGIN; never add sleep in a transaction or alter SOS
// pool reservations. Recover gradually after two quiet observation windows.
func (c *mixedCommitController) run(ctx context.Context, db *sql.DB) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var prior uint64
	havePrior := false
	quiet := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		var total uint64
		queryCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		err := db.QueryRowContext(queryCtx, `SELECT COALESCE(SUM(SUM_TIMER_WAIT),0) FROM performance_schema.events_waits_summary_global_by_event_name WHERE EVENT_NAME LIKE 'wait/io/file/innodb/%log%' OR EVENT_NAME LIKE 'wait/io/file/sql/binlog%'`).Scan(&total)
		cancel()
		commit, sql, ok := c.sample()
		if err != nil {
			havePrior = false
			quiet = 0
			continue
		}
		if !havePrior || total < prior {
			prior = total
			havePrior = true
			continue
		}
		wait := time.Duration((total - prior) / 1000)
		prior = total
		if !ok {
			quiet = 0
			continue
		}
		a := c.admission
		a.mu.Lock()
		limit := a.limit
		// Commit-dominated windows with substantial log file waits are
		// congestion candidates. A slow SQL phase alone is not a reason
		// to shrink the transaction budget.
		if c.enabled && commit > 8*time.Millisecond && commit*2 > sql && wait > 100*time.Millisecond {
			quiet = 0
			limit = max(4, limit-1)
		} else if c.enabled && commit < 5*time.Millisecond {
			quiet++
			if quiet >= 2 {
				limit = min(healthWriteConcurrency, limit+1)
				quiet = 0
			}
		} else {
			quiet = 0
		}
		if limit != a.limit {
			a.limit = limit
			a.urgentLimit = min(limit, healthWriteUrgentConcurrency)
			a.signalLocked()
		}
		a.mu.Unlock()
		c.changes = append(c.changes, mixedFeedbackChange{limit, float64(commit) / 1e6, float64(sql) / 1e6, float64(wait) / 1e6})
	}
}
