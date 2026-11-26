package care

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeliveryPoolHasBoundedConcurrencyAndCancels(t *testing.T) {
	p := testCareDB(t)
	ctx := context.Background()
	if _, err := p.ExecContext(ctx, "INSERT INTO care_notification_outbox(incident_id,channel,recipient) VALUES(1,'sms','13800000001'),(1,'voice','13800000001')"); err != nil {
		t.Fatal(err)
	}
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
		}
		arrived <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	d := dispatcher{db: p, webhookURL: server.URL, webhookToken: "test", httpClient: server.Client()}
	worker, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); d.runDispatchers(worker, 2) }()
	defer func() { cancel(); <-done }()
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(3 * time.Second):
			t.Fatal("independent notifications serialized behind provider")
		}
	}
	if peak.Load() != 2 {
		t.Fatal("worker bound", peak.Load())
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatchers survived cancellation")
	}
}

func TestDeliveryConcurrencyIsBounded(t *testing.T) {
	for _, v := range []struct {
		value string
		want  int
	}{{"", 8}, {"0", 8}, {"65", 8}, {"1", 1}, {"64", 64}} {
		t.Setenv("CARE_NOTIFY_CONCURRENCY", v.value)
		if got := deliveryConcurrency(); got != v.want {
			t.Fatal(v.value, got)
		}
	}
}
