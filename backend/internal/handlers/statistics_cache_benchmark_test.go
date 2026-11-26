package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/health"
	"yihe-care/internal/session"
)

// BenchmarkHealthStatisticsHybrid measures the complete statistics handler over
// persisted history. "disk" disables the application cache; MySQL's own buffer
// pool remains enabled, so it does not represent physical cold-disk throughput.
func BenchmarkHealthStatisticsHybrid(b *testing.B) {
	pool := benchmarkStatisticsDatabase(b)
	ctx := context.Background()
	if _, err := pool.ExecContext(ctx, "INSERT INTO users(user_id,user_name,role) VALUES(11,'bench','1')"); err != nil {
		b.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(11,'supervisor',11)"); err != nil {
		b.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, "INSERT INTO oldperson(old_id,name) VALUES(1,'benchmark')"); err != nil {
		b.Fatal(err)
	}
	from := time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)
	for start := 0; start < 10000; start += 200 {
		samples := make([]health.Sample, 200)
		for i := range samples {
			at := from.Add(time.Duration(start+i) * time.Second)
			sample, err := (health.Sample{
				RecordID: strconv.Itoa(start + i), Source: "watch", SourceDevice: "BENCHMARK",
				Metric: "heart_rate", Semantics: "instant", Unit: "count/min",
				Value: health.Number(72), Start: at, End: at, Version: 1,
			}).Normalize()
			if err != nil {
				b.Fatal(err)
			}
			samples[i] = sample
		}
		tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			b.Fatal(err)
		}
		if _, _, err = health.SaveBatch(ctx, tx, health.Owner{OldID: 1}, samples); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			tx.Rollback()
			b.Fatal(err)
		}
	}
	query, err := json.Marshal(map[string]any{
		"oldId": 1, "source": "watch", "sourceDevice": "BENCHMARK",
		"metric": "heart_rate", "semantics": "instant", "from": from, "to": from.Add(24 * time.Hour),
	})
	if err != nil {
		b.Fatal(err)
	}
	for _, mode := range []struct {
		name  string
		bytes int64
	}{{"disk", 0}, {"hot", 16 << 20}} {
		b.Run(mode.name, func(b *testing.B) {
			h := New(pool, session.NewStore(time.Hour), config.Config{HealthCacheBytes: mode.bytes})
			b.Cleanup(h.Sessions.Close)
			handler := withHealthPrincipal(h, session.Principal{Role: "ADMIN", ID: 11}, h.healthStatistics)
			request := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/health/statistics", bytes.NewReader(query))
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				return w
			}
			warm := request()
			var result struct {
				ReturnCode string `json:"returnCode"`
				Statistics struct {
					Count int     `json:"count"`
					Mean  float64 `json:"mean"`
				} `json:"statistics"`
			}
			if err := json.Unmarshal(warm.Body.Bytes(), &result); err != nil || warm.Code != http.StatusOK || result.ReturnCode != "1111" || result.Statistics.Count != 10000 || result.Statistics.Mean != 72 {
				b.Fatalf("invalid statistics: HTTP %d, %s, %v", warm.Code, warm.Body.String(), err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				w := request()
				if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"count":10000`)) || !bytes.Contains(w.Body.Bytes(), []byte(`"mean":72`)) {
					b.Fatalf("invalid statistics: HTTP %d, %s", w.Code, w.Body.String())
				}
			}
		})
	}
}

func benchmarkStatisticsDatabase(b *testing.B) *db.Database {
	b.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		b.Skip("TEST_MYSQL_DSN not set")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		b.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		b.Fatal(err)
	}
	name := "yihe_bench_" + uuid.NewString()[:8]
	if _, err = admin.ExecContext(context.Background(), "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"); err != nil {
		admin.Close()
		b.Fatal(err)
	}
	var pool *db.Database
	b.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		_, err := admin.ExecContext(context.Background(), "DROP DATABASE `"+name+"`")
		admin.Close()
		if err != nil {
			b.Error(err)
		}
	})
	cfg.DBName = name
	pool, err = db.OpenMySQL(context.Background(), cfg.FormatDSN())
	if err != nil {
		b.Fatal(err)
	}
	if err = db.MigrateMySQL(context.Background(), pool); err != nil {
		b.Fatal(err)
	}
	return pool
}
