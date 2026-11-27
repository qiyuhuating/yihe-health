package observability

import (
	"fmt"
	"github.com/google/uuid"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	mu             sync.Mutex
	values         map[string]float64
	httpHistograms map[httpLabels]*httpHistogram
	resolve        func(*http.Request) string
}

func New() *Registry {
	return &Registry{values: map[string]float64{}, httpHistograms: make(map[httpLabels]*httpHistogram)}
}

var Default = New()

func (r *Registry) SetRouteResolver(f func(*http.Request) string) {
	r.mu.Lock()
	r.resolve = f
	r.mu.Unlock()
}
func (r *Registry) Add(name string, delta float64) {
	r.mu.Lock()
	r.values[name] += delta
	r.mu.Unlock()
}
func (r *Registry) Set(name string, value float64) {
	r.mu.Lock()
	r.values[name] = value
	r.mu.Unlock()
}

// Keep numeric observations contiguous. Only the first matching bucket is
// incremented; cumulative Prometheus buckets are reconstructed during scrape.
// Labels come from the bounded route/method/status resolver, never raw URLs.
var httpBucketBounds = [...]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 15, 20, 30, 60}

type httpHistogram struct {
	buckets [len(httpBucketBounds)]uint64
	count   uint64
	sum     float64
	// Immutable names are built once per label set. Snapshots share only
	// these names, never mutable counters or private request data.
	keys *[len(httpBucketBounds) + 4]string
}

type httpLabels struct {
	route, method string
	status        int
}

func newHTTPHistogram(key httpLabels) *httpHistogram {
	labels := `route=` + strconv.Quote(key.route) + `,method=` + strconv.Quote(key.method) + `,status=` + strconv.Quote(strconv.Itoa(key.status))
	keys := new([len(httpBucketBounds) + 4]string)
	keys[0] = "yihe_http_requests_total{" + labels + "}"
	keys[1] = "yihe_http_duration_seconds_count{" + labels + "}"
	keys[2] = "yihe_http_duration_seconds_sum{" + labels + "}"
	for i, upper := range httpBucketBounds {
		keys[3+i] = `yihe_http_duration_seconds_bucket{` + labels + `,le=` + strconv.Quote(strconv.FormatFloat(upper, 'g', -1, 64)) + `}`
	}
	keys[len(keys)-1] = `yihe_http_duration_seconds_bucket{` + labels + `,le="+Inf"}`
	return &httpHistogram{keys: keys}
}

func (r *Registry) observeHTTP(labels httpLabels, seconds float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.httpHistograms[labels]
	if h == nil {
		h = newHTTPHistogram(labels)
		r.httpHistograms[labels] = h
	}
	h.count++
	h.sum += seconds
	for i, upper := range httpBucketBounds {
		if seconds <= upper {
			h.buckets[i]++
			break
		}
	}
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# TYPE yihe_http_requests_total counter\n# TYPE yihe_http_duration_seconds histogram\n# TYPE yihe_http_in_flight gauge")
	// Snapshot under lock, then release it before writing to an arbitrary
	// network client. A slow or disconnected scraper must not block request
	// completion, counters, or dispatcher heartbeat updates.
	r.mu.Lock()
	values := maps.Clone(r.values)
	// Copy numeric state under lock. Formatting and network writes happen
	// after unlock so slow scrapers cannot block request completion.
	histograms := make([]httpHistogram, 0, len(r.httpHistograms))
	for _, h := range r.httpHistograms {
		histograms = append(histograms, *h)
	}
	r.mu.Unlock()
	type point struct {
		key   string
		value float64
	}
	points := make([]point, 0, len(values)+len(histograms)*(len(httpBucketBounds)+4))
	put := func(key string, value float64) {
		// Histogram observations take precedence over manually set names,
		// matching the previous map-based exposition.
		delete(values, key)
		points = append(points, point{key, value})
	}
	for _, h := range histograms {
		put(h.keys[0], float64(h.count))
		put(h.keys[1], float64(h.count))
		put(h.keys[2], h.sum)
		var cumulative uint64
		for i, count := range h.buckets {
			cumulative += count
			put(h.keys[3+i], float64(cumulative))
		}
		put(h.keys[len(h.keys)-1], float64(h.count))
	}
	for key, value := range values {
		points = append(points, point{key, value})
	}
	slices.SortFunc(points, func(a, b point) int { return strings.Compare(a.key, b.key) })
	line := make([]byte, 0, 256)
	for _, p := range points {
		line = append(line[:0], p.key...)
		line = append(line, ' ')
		line = strconv.AppendFloat(line, p.value, 'g', -1, 64)
		line = append(line, '\n')
		if _, err := w.Write(line); err != nil {
			return
		}
	}
}

type writer struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *writer) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *writer) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}
func (w *writer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (r *Registry) HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		id := uuid.NewString()
		req.Header.Set("X-Request-ID", id)
		if strings.HasPrefix(req.URL.Path, "/api/v1/") {
			req = withRequestAudit(req)
		}
		w.Header().Set("X-Request-ID", id)
		start := time.Now()
		r.Add("yihe_http_in_flight", 1)
		defer r.Add("yihe_http_in_flight", -1)
		out := &writer{ResponseWriter: w}
		next.ServeHTTP(out, req)
		if out.status == 0 {
			out.status = 200
		}
		r.mu.Lock()
		resolver := r.resolve
		r.mu.Unlock()
		route := "unknown"
		if resolver != nil {
			if pattern := resolver(req); pattern != "" {
				route = pattern
			}
		}
		switch req.URL.Path {
		case "/metrics":
			route = "/metrics"
		case "/healthz":
			route = "/healthz"
		case "/openapi.json":
			route = "/openapi.json"
		}
		if strings.HasPrefix(req.URL.Path, "/static/") || strings.HasPrefix(req.URL.Path, "/assets/") {
			route = "static"
		}
		seconds := time.Since(start).Seconds()
		method := "OTHER"
		switch req.Method {
		case "GET":
			method = "GET"
		case "HEAD":
			method = "HEAD"
		case "POST":
			method = "POST"
		case "PUT":
			method = "PUT"
		case "PATCH":
			method = "PATCH"
		case "DELETE":
			method = "DELETE"
		case "OPTIONS":
			method = "OPTIONS"
		case "CONNECT":
			method = "CONNECT"
		case "TRACE":
			method = "TRACE"
		}
		r.observeHTTP(httpLabels{route, method, out.status}, seconds)
		logHTTPRequest(req, id, route, method, out.status, seconds*1000, out.bytes)
	})
}
