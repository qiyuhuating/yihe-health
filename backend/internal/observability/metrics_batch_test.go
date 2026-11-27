package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPHistogramLongTailAndCumulativeBuckets(t *testing.T) {
	r := New()
	labels := `route="/api/v1/device/healthBatch",method="POST",status="200"`
	r.observeHTTP(httpLabels{"/api/v1/device/healthBatch", "POST", 200}, 14.841)
	r.observeHTTP(httpLabels{"/api/v1/device/healthBatch", "POST", 200}, 0.02)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	text := w.Body.String()
	for _, expected := range []string{
		`yihe_http_requests_total{` + labels + `} 2`,
		`yihe_http_duration_seconds_count{` + labels + `} 2`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="10"} 1`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="15"} 2`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="20"} 2`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="30"} 2`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="60"} 2`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="+Inf"} 2`,
		`yihe_http_duration_seconds_bucket{` + labels + `,le="0.005"} 0`,
	} {
		if !strings.Contains(text, expected+"\n") {
			t.Fatalf("missing cumulative histogram point %q in output: %s", expected, text)
		}
	}
}

func TestHTTPHistogramConcurrentUpdates(t *testing.T) {
	r := New()
	labels := `route="unknown",method="POST",status="500"`
	const workers, each = 8, 100
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				r.observeHTTP(httpLabels{"unknown", "POST", 500}, 17)
			}
		}()
	}
	wg.Wait()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	for _, line := range []string{
		"yihe_http_duration_seconds_count{" + labels + "} 800\n",
		`yihe_http_duration_seconds_bucket{` + labels + `,le="15"} 0` + "\n",
		`yihe_http_duration_seconds_bucket{` + labels + `,le="20"} 800` + "\n",
	} {
		if !strings.Contains(w.Body.String(), line) {
			t.Fatalf("missing %q", line)
		}
	}
}

func BenchmarkRegistryObserveHTTP(b *testing.B) {
	r := New()
	labels := httpLabels{"/api/v1/device/healthBatch", "POST", 200}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r.observeHTTP(labels, 0.125)
		}
	})
}

// blockedMetricsWriter simulates a Prometheus client that stopped reading.
// It blocks on the first metric line, *after* the explanatory header.
type blockedMetricsWriter struct {
	header  http.Header
	writes  int
	blocked chan struct{}
	release chan struct{}
}

func (w *blockedMetricsWriter) Header() http.Header { return w.header }
func (w *blockedMetricsWriter) WriteHeader(_ int)   {}
func (w *blockedMetricsWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes == 2 {
		close(w.blocked)
		<-w.release
	}
	return len(b), nil
}

func TestSlowMetricsScraperDoesNotBlockRegistryUpdates(t *testing.T) {
	r := New()
	r.Set("yihe_care_worker_last_heartbeat_unixtime", 10)
	w := &blockedMetricsWriter{
		header: make(http.Header), blocked: make(chan struct{}), release: make(chan struct{}),
	}
	defer close(w.release)
	scraped := make(chan struct{})
	go func() {
		defer close(scraped)
		r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	}()
	select {
	case <-w.blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("metrics scrape never reached the blocked writer")
	}
	updated := make(chan struct{})
	go func() {
		r.Add("yihe_care_worker_last_heartbeat_unixtime", 1)
		close(updated)
	}()
	select {
	case <-updated:
	case <-time.After(2 * time.Second):
		t.Fatal("a stalled metrics client held the registry mutex")
	}
	// Existing in-flight scrape remains a consistent snapshot.
	r.mu.Lock()
	got := r.values["yihe_care_worker_last_heartbeat_unixtime"]
	r.mu.Unlock()
	if got != 11 {
		t.Fatalf("registry update was lost: %v", got)
	}
}
