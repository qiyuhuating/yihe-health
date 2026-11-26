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

func (a *healthWriteAdmission) acquire(ctx context.Context, device string, owner ...int64) bool {
	if ctx.Err() != nil {
		return false
	}
	a.mu.Lock()
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
	timer := time.NewTimer(healthWriteQueueWait)
	defer timer.Stop()
	for {
		if a.changed == nil {
			a.changed = make(chan struct{})
		}
		changed := a.changed
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-changed:
		}
		a.mu.Lock()
		if ctx.Err() != nil {
			a.mu.Unlock()
			return false
		}
		select {
		case <-timer.C:
			a.mu.Unlock()
			return false
		default:
		}
		if a.tryAcquire(device, owner...) {
			a.mu.Unlock()
			return true
		}
	}
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
	delete(a.active, device)
	if owner, ok := a.domains[device]; ok {
		delete(a.owners, owner)
		delete(a.domains, device)
	}
	a.signalLocked()
	a.mu.Unlock()
}

func (a *healthWriteAdmission) signalLocked() {
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
