package lane

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func startClusterNode(t *testing.T, name string, port int, routes []*url.URL) *server.Server {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		ServerName: name, Host: "127.0.0.1", Port: -1,
		JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true, SyncAlways: true,
		Cluster: server.ClusterOpts{Name: "yihe-test", Host: "127.0.0.1", Port: port},
		Routes:  routes,
	})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })
	if !s.ReadyForConnections(10 * time.Second) {
		t.Fatal("cluster node did not start")
	}
	return s
}

func eventually(t *testing.T, ctx context.Context, description string, check func() bool) {
	t.Helper()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if check() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for %s: %v", description, ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestReplicatedEventSurvivesStreamLeaderShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	// JetStream requires configured routes even on the first node. Reserve
	// three distinct local ports before starting any cluster member.
	var listeners [3]net.Listener
	var ports [3]int
	var routes []*url.URL
	for i := range listeners {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		listeners[i] = listener
		ports[i] = listener.Addr().(*net.TCPAddr).Port
		routes = append(routes, &url.URL{Scheme: "nats-route", Host: listener.Addr().String()})
	}
	for _, listener := range listeners {
		_ = listener.Close()
	}
	var nodes []*server.Server
	for i, port := range ports {
		nodes = append(nodes, startClusterNode(t, fmt.Sprintf("node-%d", i+1), port, routes))
	}
	eventually(t, ctx, "three metadata peers", func() bool {
		for _, node := range nodes {
			if node.JetStreamIsLeader() && len(node.JetStreamClusterPeers()) == 3 {
				return true
			}
		}
		return false
	})

	addresses := fmt.Sprintf("%s,%s,%s", nodes[0].ClientURL(), nodes[1].ClientURL(), nodes[2].ClientURL())
	nc, err := nats.Connect(addresses, nats.MaxReconnects(-1), nats.ReconnectWait(50*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	l, err := Open(ctx, nc, SOS, 3, 1024*1024, 100)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := l.js.Stream(ctx, "YIHE_sos")
	if err != nil {
		t.Fatal(err)
	}
	var leader string
	eventually(t, ctx, "two current stream replicas", func() bool {
		info, err := stream.Info(ctx)
		if err != nil || info.Cluster == nil || info.Cluster.Leader == "" || len(info.Cluster.Replicas) != 2 {
			return false
		}
		for _, replica := range info.Cluster.Replicas {
			if !replica.Current || replica.Offline {
				return false
			}
		}
		leader = info.Cluster.Leader
		return true
	})
	if _, err := l.Publish(ctx, []byte("synthetic-quorum-sos")); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	stopped := false
	for _, node := range nodes {
		if node.Name() == leader {
			node.Shutdown()
			node.WaitForShutdown()
			stopped = true
			break
		}
	}
	if !stopped {
		t.Fatalf("stream leader %q is not a test cluster member", leader)
	}
	eventually(t, ctx, "replacement stream leader", func() bool {
		probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
		defer stop()
		info, err := stream.Info(probe)
		return err == nil && info.Cluster != nil && info.Cluster.Leader != "" && info.Cluster.Leader != leader
	})
	eventually(t, ctx, "recover replicated event", func() bool {
		err := l.CommitOne(ctx, func(_ context.Context, data []byte) error {
			if string(data) != "synthetic-quorum-sos" {
				t.Fatalf("unexpected recovered payload: %q", data)
			}
			return nil
		})
		return err == nil
	})
	// Verify the remaining quorum can also accept a new event.
	eventually(t, ctx, "publish after leader loss", func() bool {
		probe, stop := context.WithTimeout(ctx, 500*time.Millisecond)
		defer stop()
		ack, err := l.js.Publish(probe, "yihe.next.sos", []byte("synthetic-after-loss"), jetstream.WithMsgID("synthetic-after-loss"))
		return err == nil && ack != nil
	})
	t.Logf("leader shutdown to confirmed new publish: %s", time.Since(started))

	// A single remaining replica cannot acknowledge a new publish.
	var remaining *server.Server
	for _, node := range nodes {
		if node.Name() == leader {
			continue
		}
		if remaining == nil {
			remaining = node
			continue
		}
		node.Shutdown()
		node.WaitForShutdown()
	}
	// Connect directly to the sole remaining node instead of observing a
	// transient client reconnect error from a connection to the stopped node.
	soloNC, err := nats.Connect(remaining.ClientURL(), nats.NoReconnect())
	if err != nil {
		t.Fatal(err)
	}
	defer soloNC.Close()
	soloJS, err := jetstream.New(soloNC)
	if err != nil {
		t.Fatal(err)
	}
	probe, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	if ack, err := soloJS.Publish(probe, "yihe.next.sos", []byte("synthetic-no-quorum")); err == nil {
		t.Fatalf("minority replica falsely acknowledged publish: %v", ack)
	}
}
