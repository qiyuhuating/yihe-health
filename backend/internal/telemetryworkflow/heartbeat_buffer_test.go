package telemetryworkflow

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/observability"
)

type heartbeatStoreStub struct {
	calls int
	query string
	args  []any
	fail  bool
}

func (s *heartbeatStoreStub) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.calls++
	s.query = query
	s.args = append([]any(nil), args...)
	if s.fail {
		return nil, errors.New("write failed")
	}
	return driver.RowsAffected(0), nil
}

func TestHeartbeatBufferCoalescesLatestState(t *testing.T) {
	store := &heartbeatStoreStub{}
	buffer := NewHeartbeatBuffer(store)
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	battery := 71
	version := "2.1.0"
	buffer.Record(context.Background(), 7, 1, &battery, nil, base)
	buffer.Record(context.Background(), 7, 1, nil, &version, base.Add(5*time.Second))
	buffer.Record(context.Background(), 8, 1, nil, nil, base.Add(2*time.Second))
	if n, err := buffer.Flush(context.Background()); err != nil || n != 2 {
		t.Fatalf("Flush() = %d, %v; want 2, nil", n, err)
	}
	if store.calls != 1 || len(store.args) != 10 || !strings.Contains(store.query, "UPDATE device d JOIN") {
		t.Fatalf("unexpected batch: calls=%d args=%d query=%q", store.calls, len(store.args), store.query)
	}
	values := map[int64][]any{}
	for i := 0; i < len(store.args); i += 5 {
		values[store.args[i].(int64)] = store.args[i : i+5]
	}
	if got := values[7]; got[1] != int64(1) || got[2] != base.Add(5*time.Second) || got[3] != 71 || got[4] != "2.1.0" {
		t.Fatalf("device 7 state=%#v", got)
	}
}

func TestHeartbeatBufferRequeuesFailedBatch(t *testing.T) {
	store := &heartbeatStoreStub{fail: true}
	buffer := NewHeartbeatBuffer(store)
	buffer.Record(context.Background(), 7, 1, nil, nil, time.Now())
	if n, err := buffer.Flush(context.Background()); err == nil || n != 0 {
		t.Fatalf("failed Flush() = %d, %v", n, err)
	}
	store.fail = false
	if n, err := buffer.Flush(context.Background()); err != nil || n != 1 {
		t.Fatalf("retry Flush() = %d, %v; want 1, nil", n, err)
	}
}

func TestHeartbeatBufferFailedShutdownFlushIsObservable(t *testing.T) {
	store := &heartbeatStoreStub{fail: true}
	buffer := NewHeartbeatBuffer(store)
	if err := buffer.Record(context.Background(), 7, 1, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	buffer.Run(ctx)
	if store.calls != 1 || len(buffer.items) != 1 {
		t.Fatalf("shutdown failed to retain unflushed heartbeat: calls=%d pending=%d", store.calls, len(buffer.items))
	}
	metrics := httptest.NewRecorder()
	observability.Default.ServeHTTP(metrics, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(metrics.Body.String(), "yihe_heartbeat_flush_errors_total ") {
		t.Fatal("failed heartbeat flush was not exposed to monitoring")
	}
	store.fail = false
	if n, err := buffer.Flush(context.Background()); err != nil || n != 1 {
		t.Fatalf("retry after shutdown error = %d, %v; want 1, nil", n, err)
	}
}

func TestHeartbeatBufferRequeuePrefersNewestMetadata(t *testing.T) {
	buffer := NewHeartbeatBuffer(&heartbeatStoreStub{})
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	newBattery, oldBattery := 90, 20
	newVersion, oldVersion := "3.0", "2.0"
	buffer.Record(context.Background(), 7, 1, &newBattery, &newVersion, base.Add(time.Minute))
	buffer.requeue([]heartbeatState{{
		deviceID: 7, bindingEpoch: 1, seenAt: base, battery: oldBattery, hasBattery: true,
		appVersion: oldVersion, hasVersion: true,
	}})
	state := buffer.items[7]
	if state.battery != newBattery || state.appVersion != newVersion || !state.seenAt.Equal(base.Add(time.Minute)) {
		t.Fatalf("older retry regressed state: %+v", state)
	}

	buffer.requeue([]heartbeatState{{
		deviceID: 7, bindingEpoch: 1, seenAt: base.Add(2 * time.Minute), battery: 80, hasBattery: true,
		appVersion: "4.0", hasVersion: true,
	}})
	state = buffer.items[7]
	if state.battery != 80 || state.appVersion != "4.0" || !state.seenAt.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("newer retry was discarded: %+v", state)
	}
}

func TestHeartbeatBatchSQLFiltersStaleRows(t *testing.T) {
	query := heartbeatBatchSQL(1)
	if !strings.Contains(query, "WHERE d.health_epoch=h.binding_epoch AND h.seen_at>=COALESCE(d.last_online_at,h.seen_at)") {
		t.Fatalf("heartbeat UPDATE must reject stale rows before assignments: %s", query)
	}
}

func TestHeartbeatBufferSeparatesBindingEpochs(t *testing.T) {
	store := &heartbeatStoreStub{}
	buffer := NewHeartbeatBuffer(store)
	base := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	oldBattery, newBattery := 10, 90
	buffer.Record(context.Background(), 7, 1, &oldBattery, nil, base)
	buffer.Record(context.Background(), 7, 2, &newBattery, nil, base.Add(time.Second))
	buffer.Record(context.Background(), 7, 1, &oldBattery, nil, base.Add(2*time.Second))
	buffer.requeue([]heartbeatState{
		{deviceID: 7, bindingEpoch: 1, seenAt: base.Add(3 * time.Second), battery: oldBattery, hasBattery: true},
	})
	state := buffer.items[7]
	if state.bindingEpoch != 2 || state.battery != newBattery || !state.seenAt.Equal(base.Add(time.Second)) {
		t.Fatalf("old binding contaminated current state: %+v", state)
	}
	if n, err := buffer.Flush(context.Background()); err != nil || n != 1 {
		t.Fatalf("Flush() = %d, %v; want 1, nil", n, err)
	}
	if len(store.args) != 5 || store.args[1] != int64(2) || store.args[3] != newBattery {
		t.Fatalf("unexpected epoch-fenced SQL arguments: %+v", store.args)
	}
}

func TestHeartbeatBufferCapacityUsesFencedDirectWrite(t *testing.T) {
	store := &heartbeatStoreStub{}
	buffer := NewHeartbeatBuffer(store)
	for id := int64(1); id <= heartbeatBufferCapacity; id++ {
		buffer.items[id] = heartbeatState{deviceID: id, bindingEpoch: 1}
	}
	battery := 75
	if err := buffer.Record(context.Background(), heartbeatBufferCapacity+1, 2, &battery, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(buffer.items) != heartbeatBufferCapacity || store.calls != 1 {
		t.Fatalf("unbounded buffer or missing backpressure: size=%d calls=%d", len(buffer.items), store.calls)
	}
	if !strings.Contains(store.query, "health_epoch=?") || store.args[7] != int64(2) {
		t.Fatalf("direct overflow write lacks binding fence: %s %#v", store.query, store.args)
	}
	store.fail = true
	if err := buffer.Record(context.Background(), heartbeatBufferCapacity+2, 2, nil, nil, time.Now()); err == nil {
		t.Fatal("failed overflow write must not be acknowledged")
	}
}

func TestHeartbeatCapacityIncludesInflightBatch(t *testing.T) {
	store := &heartbeatStoreStub{}
	buffer := NewHeartbeatBuffer(store)
	for id := int64(1); id <= heartbeatBufferCapacity; id++ {
		buffer.items[id] = heartbeatState{deviceID: id, bindingEpoch: 1}
	}
	batch := buffer.drain(heartbeatFlushBatch)
	if len(batch) != heartbeatFlushBatch || buffer.inflight != heartbeatFlushBatch {
		t.Fatalf("invalid in-flight reservation: size=%d inflight=%d", len(batch), buffer.inflight)
	}
	if err := buffer.Record(context.Background(), heartbeatBufferCapacity+1, 1, nil, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || len(buffer.items)+buffer.inflight != heartbeatBufferCapacity {
		t.Fatalf("in-flight capacity was ignored: calls=%d entries=%d inflight=%d", store.calls, len(buffer.items), buffer.inflight)
	}
	buffer.requeue(batch)
	buffer.finishDrain(len(batch))
	if len(buffer.items) != heartbeatBufferCapacity || buffer.inflight != 0 {
		t.Fatalf("failed batch lost capacity accounting: entries=%d inflight=%d", len(buffer.items), buffer.inflight)
	}
}

func TestHeartbeatBatchShape(t *testing.T) {
	for count := 1; count <= heartbeatFlushBatch; count++ {
		query := heartbeatBatchSQL(count)
		if strings.Count(query, "?") != count*5 || strings.Count(query, " UNION ALL SELECT ") != count-1 {
			t.Fatalf("invalid SQL shape for %d rows", count)
		}
		if query != buildHeartbeatBatchSQL(count) {
			t.Fatalf("cached query changed for %d rows", count)
		}
	}
}

func BenchmarkHeartbeatBatchSQL(b *testing.B) {
	for _, count := range []int{1, 32, heartbeatFlushBatch} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			heartbeatBatchSQL(count)
			b.ReportAllocs()
			for b.Loop() {
				_ = heartbeatBatchSQL(count)
			}
		})
	}
}
