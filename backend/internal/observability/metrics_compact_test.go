package observability

import (
	"math"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func scrapeValues(r *Registry) map[string]string {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	values := make(map[string]string)
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if i := strings.LastIndexByte(line, ' '); i > 0 && line[0] != '#' {
			values[line[:i]] = line[i+1:]
		}
	}
	return values
}

func TestCompactHistogramMatchesReference(t *testing.T) {
	// Compare public exposition to the former cumulative update algorithm,
	// including exact boundaries, adjacent floats and overflow observations.
	bounds := [...]float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 15, 20, 30, 60}
	r := New()
	for _, labels := range []string{`route="a",method="GET",status="200"`, `route="b",method="POST",status="500"`} {
		observations := []float64{-1, 0, 61, math.Inf(1), math.NaN()}
		for _, bound := range bounds {
			observations = append(observations, math.Nextafter(bound, 0), bound, math.Nextafter(bound, math.Inf(1)))
		}
		var counts [15]int
		var sum float64
		for _, value := range observations {
			r.observeHTTP(labels, value)
			sum += value
			for i, bound := range bounds {
				if value <= bound {
					counts[i]++
				}
			}
		}
		got := scrapeValues(r)
		for i, bound := range bounds {
			key := `yihe_http_duration_seconds_bucket{` + labels + `,le=` + strconv.Quote(strconv.FormatFloat(bound, 'g', -1, 64)) + `}`
			if got[key] != strconv.Itoa(counts[i]) {
				t.Fatalf("%s: got %s want %d", key, got[key], counts[i])
			}
		}
		for _, key := range []string{"yihe_http_requests_total{" + labels + "}", "yihe_http_duration_seconds_count{" + labels + "}", `yihe_http_duration_seconds_bucket{` + labels + `,le="+Inf"}`} {
			if got[key] != strconv.Itoa(len(observations)) {
				t.Fatalf("incorrect count: %s=%s", key, got[key])
			}
		}
		if got["yihe_http_duration_seconds_sum{"+labels+"}"] != strconv.FormatFloat(sum, 'g', -1, 64) {
			t.Fatal("incorrect sum")
		}
	}
}

func TestCompactHistogramConcurrentScrapeSnapshot(t *testing.T) {
	r := New()
	labels := `route="a",method="GET",status="200"`
	r.observeHTTP(labels, .125)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 10000 {
			r.observeHTTP(labels, .125)
		}
	}()
	for range 50 {
		values := scrapeValues(r)
		count, err := strconv.ParseFloat(values["yihe_http_duration_seconds_count{"+labels+"}"], 64)
		if err != nil {
			t.Fatal(err)
		}
		sum, err := strconv.ParseFloat(values["yihe_http_duration_seconds_sum{"+labels+"}"], 64)
		if err != nil || sum != count*.125 || values[`yihe_http_duration_seconds_bucket{`+labels+`,le="0.25"}`] != strconv.FormatFloat(count, 'g', -1, 64) {
			t.Fatal("torn histogram snapshot")
		}
	}
	wg.Wait()
}
