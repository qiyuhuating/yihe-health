package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"yihe-care/internal/csrf"
	"yihe-care/internal/session"
)

func mixedAuthRequest(client *http.Client, base, method, path, cookie, token string, payload any) (float64, []byte, string, int, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return 0, nil, "", 0, err
		}
	}
	req, err := http.NewRequest(method, base+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", 0, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	if token != "" {
		req.Header.Set(csrf.HeaderName, token)
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, "", 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	elapsed := float64(time.Since(start)) / float64(time.Millisecond)
	issued := ""
	for _, c := range resp.Cookies() {
		if c.Name == session.CookieName {
			issued = c.Value
		}
	}
	return elapsed, data, issued, resp.StatusCode, err
}

type mixedDBSnapshot struct {
	status            map[string]uint64
	commits, commitPS uint64
	buckets           map[uint64]uint64
	waits             map[string][2]uint64
}

// Only for an isolated benchmark MySQL instance. Global counters include
// any other workload, so callers must keep the server exclusive.
func readMixedDBSnapshot(t *testing.T, db *sql.DB) mixedDBSnapshot {
	t.Helper()
	s := mixedDBSnapshot{status: map[string]uint64{}, buckets: map[uint64]uint64{}, waits: map[string][2]uint64{}}
	rows, err := db.QueryContext(t.Context(), `SHOW GLOBAL STATUS WHERE Variable_name IN ('Com_commit','Innodb_os_log_fsyncs','Innodb_os_log_written','Innodb_data_fsyncs','Innodb_log_waits','Innodb_buffer_pool_wait_free','Binlog_commits','Binlog_group_commits','Innodb_data_written','Performance_schema_digest_lost','Threads_running')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var key, value string
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			rows.Close()
			t.Fatal(err)
		}
		s.status[key] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT COALESCE(SUM(COUNT_STAR),0),COALESCE(SUM(SUM_TIMER_WAIT),0) FROM performance_schema.events_statements_summary_by_digest WHERE SCHEMA_NAME=DATABASE() AND DIGEST_TEXT='COMMIT'`).Scan(&s.commits, &s.commitPS); err != nil {
		t.Fatal(err)
	}
	rows, err = db.QueryContext(t.Context(), `SELECT h.BUCKET_TIMER_HIGH,SUM(h.COUNT_BUCKET) FROM performance_schema.events_statements_histogram_by_digest h JOIN performance_schema.events_statements_summary_by_digest d ON h.SCHEMA_NAME=d.SCHEMA_NAME AND h.DIGEST=d.DIGEST WHERE d.SCHEMA_NAME=DATABASE() AND d.DIGEST_TEXT='COMMIT' GROUP BY h.BUCKET_TIMER_HIGH ORDER BY h.BUCKET_TIMER_HIGH`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var bound, count uint64
		if err = rows.Scan(&bound, &count); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		s.buckets[bound] = count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows, err = db.QueryContext(t.Context(), `SELECT EVENT_NAME,COUNT_STAR,SUM_TIMER_WAIT FROM performance_schema.events_waits_summary_global_by_event_name WHERE EVENT_NAME LIKE 'wait/io/file/innodb/%' OR EVENT_NAME LIKE 'wait/io/file/sql/binlog%' OR EVENT_NAME LIKE 'wait/synch/%/innodb/%log%' OR EVENT_NAME LIKE 'wait/synch/%/sql/%commit%'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		var count, ps uint64
		if err = rows.Scan(&name, &count, &ps); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		s.waits[name] = [2]uint64{count, ps}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (s mixedDBSnapshot) delta(before mixedDBSnapshot) map[string]any {
	counters := map[string]uint64{}
	for k, v := range s.status {
		if old, ok := before.status[k]; ok && v >= old && k != "Threads_running" {
			counters[k] = v - old
		}
	}
	waits := map[string]any{}
	for k, v := range s.waits {
		old := before.waits[k]
		if v[0] > old[0] && v[1] >= old[1] {
			waits[k] = map[string]any{"count": v[0] - old[0], "total_ms": float64(v[1]-old[1]) / 1e9}
		}
	}
	count, ps := s.commits-before.commits, s.commitPS-before.commitPS
	histogram := map[string]uint64{}
	for bound, v := range s.buckets {
		if v > before.buckets[bound] {
			histogram[strconv.FormatUint(bound, 10)] = v - before.buckets[bound]
		}
	}
	return map[string]any{"global_status_delta": counters, "schema_commit_count": count, "schema_commit_total_ms": float64(ps) / 1e9, "schema_commit_histogram_ps_upper": histogram, "global_waits": waits}
}
