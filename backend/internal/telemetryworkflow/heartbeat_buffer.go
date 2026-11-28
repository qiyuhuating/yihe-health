package telemetryworkflow

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"time"

	"yihe-care/internal/observability"
	"yihe-care/internal/telemetryrepo"
)

const heartbeatFlushInterval = 15 * time.Second
const heartbeatFlushBatch = 128
const heartbeatBufferCapacity = 8192

type heartbeatStore interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type heartbeatState struct {
	deviceID     int64
	bindingEpoch int64
	seenAt       time.Time
	battery      int
	hasBattery   bool
	appVersion   string
	hasVersion   bool
}

// HeartbeatBuffer coalesces replaceable device liveness writes.
// Safety-critical events never enter this buffer.
type HeartbeatBuffer struct {
	store    heartbeatStore
	mu       sync.Mutex
	flushMu  sync.Mutex
	items    map[int64]heartbeatState
	inflight int
}

func NewHeartbeatBuffer(store heartbeatStore) *HeartbeatBuffer {
	return &HeartbeatBuffer{store: store, items: make(map[int64]heartbeatState)}
}

func (b *HeartbeatBuffer) Record(ctx context.Context, deviceID, bindingEpoch int64, battery *int, appVersion *string, seenAt time.Time) error {
	if b == nil || b.store == nil || deviceID < 1 || bindingEpoch < 1 {
		return nil
	}
	seenAt = seenAt.UTC()
	b.mu.Lock()
	state := b.items[deviceID]
	if state.bindingEpoch > bindingEpoch {
		b.mu.Unlock()
		return nil
	}
	if state.bindingEpoch == 0 && len(b.items)+b.inflight >= heartbeatBufferCapacity {
		b.mu.Unlock()
		// Keep the in-memory projection bounded. Backpressure is paid only
		// by new device keys; an acknowledged heartbeat is never discarded.
		return telemetryrepo.TouchDevice(ctx, b.store, deviceID, bindingEpoch, battery, appVersion, seenAt)
	}
	if state.bindingEpoch != bindingEpoch {
		state = heartbeatState{deviceID: deviceID, bindingEpoch: bindingEpoch}
	}
	state.deviceID = deviceID
	newest := state.seenAt.IsZero() || !seenAt.Before(state.seenAt)
	if newest {
		state.seenAt = seenAt
		if battery != nil {
			state.battery, state.hasBattery = *battery, true
		}
		if appVersion != nil {
			state.appVersion, state.hasVersion = *appVersion, true
		}
	}
	b.items[deviceID] = state
	b.mu.Unlock()
	observability.Default.Add("yihe_heartbeat_buffered_total", 1)
	return nil
}

func (b *HeartbeatBuffer) drain(max int) []heartbeatState {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) == 0 {
		return nil
	}
	items := make([]heartbeatState, 0, min(max, len(b.items)))
	for id, state := range b.items {
		items = append(items, state)
		delete(b.items, id)
		if len(items) == max {
			break
		}
	}
	b.inflight += len(items)
	return items
}

func (b *HeartbeatBuffer) finishDrain(count int) {
	b.mu.Lock()
	b.inflight -= count
	b.mu.Unlock()
}

func (b *HeartbeatBuffer) requeue(items []heartbeatState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, stale := range items {
		current, ok := b.items[stale.deviceID]
		if !ok || stale.bindingEpoch > current.bindingEpoch {
			b.items[stale.deviceID] = stale
			continue
		}
		if stale.bindingEpoch < current.bindingEpoch {
			continue
		}
		if stale.seenAt.After(current.seenAt) {
			current.seenAt = stale.seenAt
			if stale.hasBattery {
				current.battery, current.hasBattery = stale.battery, true
			}
			if stale.hasVersion {
				current.appVersion, current.hasVersion = stale.appVersion, true
			}
		} else {
			if !current.hasBattery && stale.hasBattery {
				current.battery, current.hasBattery = stale.battery, true
			}
			if !current.hasVersion && stale.hasVersion {
				current.appVersion, current.hasVersion = stale.appVersion, true
			}
		}
		b.items[stale.deviceID] = current
	}
}

const heartbeatPrefix = "UPDATE device d JOIN ("
const heartbeatFirst = "SELECT CAST(? AS SIGNED) device_id,CAST(? AS SIGNED) binding_epoch,CAST(? AS DATETIME(6)) seen_at,CAST(? AS SIGNED) battery,CAST(? AS CHAR(20)) app_version"
const heartbeatMore = " UNION ALL SELECT CAST(? AS SIGNED),CAST(? AS SIGNED),CAST(? AS DATETIME(6)),CAST(? AS SIGNED),CAST(? AS CHAR(20))"

// Filter stale observations before SET; multi-table UPDATE assignment order is unspecified.
const heartbeatSuffix = ") h ON h.device_id=d.device_id SET d.last_time=GREATEST(COALESCE(d.last_time,h.seen_at),h.seen_at),d.last_online_at=GREATEST(COALESCE(d.last_online_at,h.seen_at),h.seen_at),d.battery=COALESCE(h.battery,d.battery),d.app_version=COALESCE(h.app_version,d.app_version) WHERE d.health_epoch=h.binding_epoch AND h.seen_at>=COALESCE(d.last_online_at,h.seen_at)"

var heartbeatSingleSQL = sync.OnceValue(func() string { return buildHeartbeatBatchSQL(1) })
var heartbeatFullSQL = sync.OnceValue(func() string { return buildHeartbeatBatchSQL(heartbeatFlushBatch) })

func heartbeatBatchSQL(count int) string {
	switch count {
	case 1:
		return heartbeatSingleSQL()
	case heartbeatFlushBatch:
		return heartbeatFullSQL()
	default:
		return buildHeartbeatBatchSQL(count)
	}
}

func buildHeartbeatBatchSQL(count int) string {
	var q strings.Builder
	q.Grow(len(heartbeatPrefix) + len(heartbeatSuffix) + len(heartbeatFirst) + (count-1)*len(heartbeatMore))
	q.WriteString(heartbeatPrefix)
	q.WriteString(heartbeatFirst)
	for i := 1; i < count; i++ {
		q.WriteString(heartbeatMore)
	}
	q.WriteString(heartbeatSuffix)
	return q.String()
}

func (b *HeartbeatBuffer) flushBatch(ctx context.Context, items []heartbeatState) error {
	args := make([]any, 0, len(items)*5)
	for _, item := range items {
		var battery, version any
		if item.hasBattery {
			battery = item.battery
		}
		if item.hasVersion {
			version = item.appVersion
		}
		args = append(args, item.deviceID, item.bindingEpoch, item.seenAt, battery, version)
	}
	result, err := b.store.ExecContext(ctx, heartbeatBatchSQL(len(items)), args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	observability.Default.Add("yihe_heartbeat_db_rows_total", float64(affected))
	observability.Default.Add("yihe_heartbeat_flush_batches_total", 1)
	return nil
}

func (b *HeartbeatBuffer) Flush(ctx context.Context) (int, error) {
	b.flushMu.Lock()
	defer b.flushMu.Unlock()
	total := 0
	for {
		items := b.drain(heartbeatFlushBatch)
		if len(items) == 0 {
			return total, nil
		}
		if err := b.flushBatch(ctx, items); err != nil {
			observability.Default.Add("yihe_heartbeat_flush_errors_total", 1)
			b.requeue(items)
			b.finishDrain(len(items))
			return total, err
		}
		b.finishDrain(len(items))
		total += len(items)
	}
}

func (b *HeartbeatBuffer) Run(ctx context.Context) {
	ticker := time.NewTicker(heartbeatFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			final, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			if _, err := b.Flush(final); err != nil {
				slog.Error("heartbeat_flush_failed", "phase", "shutdown", "error", err)
			}
			cancel()
			return
		case <-ticker.C:
			run, cancel := context.WithTimeout(ctx, 5*time.Second)
			if _, err := b.Flush(run); err != nil {
				slog.Warn("heartbeat_flush_failed", "phase", "periodic", "error", err)
			}
			cancel()
		}
	}
}
