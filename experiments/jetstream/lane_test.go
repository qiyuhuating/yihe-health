package lane

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func broker(t *testing.T, dir string) (*server.Server, *nats.Conn) {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: dir,
		NoLog: true, NoSigs: true, SyncAlways: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	t.Cleanup(func() {
		s.Shutdown()
		s.WaitForShutdown()
	})
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("broker did not start")
	}
	nc, err := nats.Connect(s.ClientURL(), nats.NoReconnect())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return s, nc
}

func openLane(t *testing.T, ctx context.Context, nc *nats.Conn, class Class, maxMessages int64) *Lane {
	t.Helper()
	l, err := Open(ctx, nc, class, 1, 1024*1024, maxMessages)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestSaturatedTelemetryDoesNotBlockSOS(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, telemetryNC := broker(t, t.TempDir())
	_, sosNC := broker(t, t.TempDir())
	telemetry := openLane(t, ctx, telemetryNC, Telemetry, 1)
	sos := openLane(t, ctx, sosNC, SOS, 2)
	first, err := telemetry.Publish(ctx, []byte("synthetic-telemetry-1"))
	if err != nil || first.Duplicate {
		t.Fatalf("first publish: %v, %v", first, err)
	}
	retry, err := telemetry.Publish(ctx, []byte("synthetic-telemetry-1"))
	if err != nil || !retry.Duplicate || retry.Sequence != first.Sequence {
		t.Fatalf("identical retry: %v, %v", retry, err)
	}
	if _, err := telemetry.Publish(ctx, []byte("synthetic-telemetry-2")); err == nil {
		t.Fatal("full stream accepted another event")
	}
	if _, err := sos.Publish(ctx, []byte("synthetic-sos-1")); err != nil {
		t.Fatalf("independent SOS broker: %v", err)
	}
	if err := sos.CommitOne(ctx, func(_ context.Context, data []byte) error {
		if string(data) != "synthetic-sos-1" {
			t.Fatalf("unexpected SOS payload: %q", data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The full telemetry stream must preserve its original, uncommitted event.
	if err := telemetry.CommitOne(ctx, func(_ context.Context, data []byte) error {
		if string(data) != "synthetic-telemetry-1" {
			t.Fatalf("unexpected telemetry payload: %q", data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestUncommittedEventSurvivesRestartAndRedelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	s, nc := broker(t, dir)
	l := openLane(t, ctx, nc, SOS, 4)
	if _, err := l.Publish(ctx, []byte("synthetic-sos-persistent")); err != nil {
		t.Fatal(err)
	}
	nc.Close()
	s.Shutdown()
	s.WaitForShutdown()
	_, recoveredNC := broker(t, dir)
	recovered := openLane(t, ctx, recoveredNC, SOS, 4)
	sinkErr := errors.New("synthetic database unavailable")
	if err := recovered.CommitOne(ctx, func(_ context.Context, data []byte) error {
		if string(data) != "synthetic-sos-persistent" {
			t.Fatalf("unexpected recovered payload: %q", data)
		}
		return sinkErr
	}); !errors.Is(err, sinkErr) {
		t.Fatalf("want sink failure: %v", err)
	}
	if err := recovered.CommitOne(ctx, func(_ context.Context, data []byte) error {
		if string(data) != "synthetic-sos-persistent" {
			t.Fatalf("unexpected redelivery payload: %q", data)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	info, err := recovered.consumer.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.NumPending != 0 || info.NumAckPending != 0 {
		t.Fatalf("unsettled consumer after confirmed commit: %d/%d", info.NumPending, info.NumAckPending)
	}
}
