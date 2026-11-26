package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Run this unchanged against both revisions. The fixture is synthetic and the
// database buffer pool stays warm; results are not production latency evidence.
func BenchmarkListPagination(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			pool := benchmarkStatisticsDatabase(b)
			ctx := context.Background()
			if _, err := pool.ExecContext(ctx, "INSERT INTO family(family_id,family_name) VALUES(1,'selected'),(2,'other')"); err != nil {
				b.Fatal(err)
			}
			for start := 1; start <= count; start += 100 {
				var customers, elders, texts []string
				for id := start; id < start+100 && id <= count; id++ {
					name, familyID := "other", 2
					if id%37 == 0 {
						name, familyID = "selected", 1
					}
					customers = append(customers, fmt.Sprintf("(%d,'%s')", id, name))
					elders = append(elders, fmt.Sprintf("(%d,'%s',%d)", id, name, familyID))
					texts = append(texts, fmt.Sprintf("(%d,'%s','%s','2026-10-19 08:01:02.123')", id, name, strings.Repeat("x", 512)))
				}
				for _, query := range []string{
					"INSERT INTO customer(customer_id,customer_name) VALUES" + strings.Join(customers, ","),
					"INSERT INTO oldperson(old_id,name,family_id) VALUES" + strings.Join(elders, ","),
					"INSERT INTO text(text_id,title,text_main,time) VALUES" + strings.Join(texts, ","),
				} {
					if _, err := pool.ExecContext(ctx, query); err != nil {
						b.Fatal(err)
					}
				}
			}
			h := testHandler(b, pool)
			for _, endpoint := range []struct {
				name, key, filterKey string
				handler              http.HandlerFunc
			}{
				{"customer", "customers", "customerName", h.listGuardians},
				{"family", "familyDtos", "familyName", h.familySelect},
				{"text", "texts", "title", h.listNotices},
			} {
				for _, test := range []struct {
					name, filter string
					page, total  int
				}{
					{"first", "", 1, count},
					{"last", "", (count + 19) / 20, count},
					{"selective", "selected", (count/37 + 19) / 20, count / 37},
					{"out_of_range", "", (count+19)/20 + 1, count},
				} {
					b.Run(endpoint.name+"/"+test.name, func(b *testing.B) {
						payload, err := json.Marshal(map[string]any{
							endpoint.filterKey: test.filter, "page": test.page, "pageSize": 20,
						})
						if err != nil {
							b.Fatal(err)
						}
						request := func() *httptest.ResponseRecorder {
							r := httptest.NewRequest(http.MethodPost, "/api/v1/benchmark", bytes.NewReader(payload))
							w := httptest.NewRecorder()
							endpoint.handler(w, r)
							return w
						}
						warm := request()
						var out map[string]any
						if err := json.Unmarshal(warm.Body.Bytes(), &out); err != nil || warm.Code != http.StatusOK || out["total"] != float64(test.total) {
							b.Fatalf("invalid page: HTTP %d %s %v", warm.Code, warm.Body.String(), err)
						}
						want := min(20, max(0, test.total-(test.page-1)*20))
						if items, ok := out[endpoint.key].([]any); !ok || len(items) != want {
							b.Fatalf("invalid page size: %s", warm.Body.String())
						}
						b.ReportAllocs()
						b.ResetTimer()
						for i := 0; i < b.N; i++ {
							if w := request(); w.Code != http.StatusOK {
								b.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
							}
						}
					})
				}
			}
		})
	}
}
