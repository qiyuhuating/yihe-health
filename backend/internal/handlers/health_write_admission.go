package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"sync"
	"time"

	"yihe-care/internal/observability"
	"yihe-care/internal/response"
)

// Reserve at most eight of the twenty business connections for health uploads
// per process. The short, bounded queue lives outside database/sql.
const healthWriteConcurrency = 8
const healthWriteUrgentConcurrency = 6
const healthWriteQueueWait = 200 * time.Millisecond

type healthWriteWaiter struct {
	device   string
	owner    []int64
	ctx      context.Context
	deadline time.Time
	ready    chan struct{}
	granted  bool
}

type healthWriteAdmission struct {
	mu              sync.Mutex
	active          map[string]bool
	owners          map[int64]struct{}
	domains         map[string]int64
	urgent          map[string]int
	activeSOS       map[string]struct{}
	activeSOSOwners map[int64]struct{}
	urgentCount     int
	waiting         chan struct{}
	waiters         []*healthWriteWaiter
	changed         chan struct{}
}

func newHealthWriteAdmission() healthWriteAdmission {
	return healthWriteAdmission{
		active:          make(map[string]bool, healthWriteConcurrency),
		owners:          make(map[int64]struct{}, healthWriteConcurrency),
		domains:         make(map[string]int64, healthWriteConcurrency),
		urgent:          make(map[string]int),
		activeSOS:       make(map[string]struct{}),
		activeSOSOwners: make(map[int64]struct{}),
		waiting:         make(chan struct{}, healthWriteConcurrency),
	}
}

// A device can hold only one local write slot, even when several batches
// arrive together. Other devices can use the remaining connections instead
// of queueing behind the same MySQL row lock. The DB still serializes replicas.
func (a *healthWriteAdmission) tryAcquire(device string, owner ...int64) bool {
	limit := healthWriteConcurrency
	if a.urgentCount > 0 {
		limit = healthWriteUrgentConcurrency
	}
	if len(a.active) >= limit || a.urgent[device] != 0 {
		return false
	}
	if _, busy := a.active[device]; busy {
		return false
	}
	if len(owner) != 0 {
		if _, busy := a.owners[owner[0]]; busy {
			return false
		}
		a.owners[owner[0]] = struct{}{}
		a.domains[device] = owner[0]
	}
	a.active[device] = false
	return true
}

// Dispatch oldest eligible requests under the lock. A blocked device or owner
// cannot block unrelated work, and new arrivals cannot steal a released slot.
func (a *healthWriteAdmission) dispatchLocked() {
	pending := a.waiters[:0]
	for _, w := range a.waiters {
		if w.ctx.Err() != nil || !time.Now().Before(w.deadline) {
			close(w.ready)
			continue
		}
		if a.tryAcquire(w.device, w.owner...) {
			w.granted = true
			close(w.ready)
			continue
		}
		pending = append(pending, w)
	}
	clear(a.waiters[len(pending):])
	a.waiters = pending
}

func (a *healthWriteAdmission) acquire(ctx context.Context, device string, owner ...int64) bool {
	if ctx.Err() != nil {
		return false
	}
	a.mu.Lock()
	a.dispatchLocked()
	if a.tryAcquire(device, owner...) {
		a.mu.Unlock()
		return true
	}
	select {
	case a.waiting <- struct{}{}:
		defer func() { <-a.waiting }()
	default:
		a.mu.Unlock()
		return false
	}
	w := &healthWriteWaiter{
		device: device, owner: owner, ctx: ctx,
		deadline: time.Now().Add(healthWriteQueueWait),
		ready:    make(chan struct{}),
	}
	a.waiters = append(a.waiters, w)
	a.mu.Unlock()
	timer := time.NewTimer(time.Until(w.deadline))
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-w.ready:
	}
	a.mu.Lock()
	if w.granted {
		if ctx.Err() == nil {
			a.mu.Unlock()
			return true
		}
		a.releaseLocked(device)
		a.signalLocked()
	} else {
		for i, pending := range a.waiters {
			if pending == w {
				copy(a.waiters[i:], a.waiters[i+1:])
				a.waiters[len(a.waiters)-1] = nil
				a.waiters = a.waiters[:len(a.waiters)-1]
				break
			}
		}
	}
	a.mu.Unlock()
	return false
}

// claimOwner upgrades a device preflight slot before opening a transaction.
// A conflicting owner must wait without either a SQL connection or a write slot.
func (a *healthWriteAdmission) claimOwner(ctx context.Context, device string, owner int64) bool {
	a.mu.Lock()
	if _, busy := a.owners[owner]; !busy && ctx.Err() == nil {
		a.owners[owner] = struct{}{}
		a.domains[device] = owner
		a.mu.Unlock()
		return true
	}
	delete(a.active, device)
	a.signalLocked()
	a.mu.Unlock()
	return a.acquire(ctx, device, owner)
}

// prioritizeSOS is called only after the authoritative credential read.
// Wait for a local upload that has claimed the transaction boundary, without
// holding a SQL connection or resident lock. A pending SOS prevents new same-device
// uploads and temporarily reserves two additional write slots. Serialize SOS by both device and resident because the transaction locks both rows.
func (a *healthWriteAdmission) prioritizeSOS(ctx context.Context, device string, owner int64) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.urgent[device]++
	a.urgentCount++
	for {
		_, sosActive := a.activeSOS[device]
		_, ownerActive := a.activeSOSOwners[owner]
		if !a.active[device] && !sosActive && !ownerActive {
			a.activeSOS[device] = struct{}{}
			a.activeSOSOwners[owner] = struct{}{}
			a.mu.Unlock()
			return func() { a.releaseSOS(device, owner, true) }, nil
		}
		if a.changed == nil {
			a.changed = make(chan struct{})
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			a.releaseSOS(device, owner, false)
			return nil, ctx.Err()
		case <-changed:
			a.mu.Lock()
			if err := ctx.Err(); err != nil {
				a.mu.Unlock()
				a.releaseSOS(device, owner, false)
				return nil, err
			}
		}
	}
}

// beginTransaction arbitrates between an admitted upload and a verified SOS
// under the same mutex. Once claimed, the upload may finish without preemption.
func (a *healthWriteAdmission) beginTransaction(device string, lockDevice bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.urgent[device] != 0 {
		return false
	}
	a.active[device] = lockDevice
	return true
}

var errHealthWriteBusy = errors.New("health write deferred for SOS")

// beginPriorityTx claims the device only after acquiring a connection. Restore
// database/sql's bounded bad-connection retry at the BEGIN boundary. No DML
// has run, and the failed connection is discarded before retrying admission.
func (a *healthWriteAdmission) beginPriorityTx(ctx context.Context, pool *sql.DB, device string, lateDeviceLock bool) (*sql.Tx, *sql.Conn, error) {
	for attempt := 0; ; attempt++ {
		conn, err := pool.Conn(ctx)
		if err != nil {
			return nil, nil, err
		}
		if !a.beginTransaction(device, !lateDeviceLock) {
			conn.Close()
			return nil, nil, errHealthWriteBusy
		}
		tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err == nil {
			return tx, conn, nil
		}
		conn.Close()
		a.finishTransaction(device)
		if !errors.Is(err, driver.ErrBadConn) || attempt == 2 {
			return nil, nil, err
		}
	}
}

func (a *healthWriteAdmission) finishTransaction(device string) {
	a.mu.Lock()
	a.active[device] = false
	a.signalLocked()
	a.mu.Unlock()
}

func (a *healthWriteAdmission) releaseSOS(device string, owner int64, active bool) {
	a.mu.Lock()
	if active {
		delete(a.activeSOS, device)
		delete(a.activeSOSOwners, owner)
	}
	if a.urgent[device] == 1 {
		delete(a.urgent, device)
	} else {
		a.urgent[device]--
	}
	a.urgentCount--
	a.signalLocked()
	a.mu.Unlock()
}

func (a *healthWriteAdmission) release(device string) {
	a.mu.Lock()
	a.releaseLocked(device)
	a.signalLocked()
	a.mu.Unlock()
}

func (a *healthWriteAdmission) releaseLocked(device string) {
	delete(a.active, device)
	if owner, ok := a.domains[device]; ok {
		delete(a.owners, owner)
		delete(a.domains, device)
	}
}

func (a *healthWriteAdmission) signalLocked() {
	a.dispatchLocked()
	if a.changed != nil {
		close(a.changed)
		a.changed = nil
	}
}

func healthWriteBusy(w http.ResponseWriter) {
	observability.Default.Add("yihe_health_write_admission_rejections_total", 1)
	w.Header().Set("Retry-After", "1")
	response.Error(w, http.StatusServiceUnavailable, "HEALTH_WRITE_BUSY", "健康数据写入繁忙，请保留待上传记录并稍后重试")
}
