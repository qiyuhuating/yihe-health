// Command reliability provides explicit maintenance and isolated test operations.
// It is never mounted as an HTTP endpoint.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"
	"yihe-care/internal/auth"
	"yihe-care/internal/blob"
	"yihe-care/internal/config"
	"yihe-care/internal/db"
	"yihe-care/internal/ratelimit"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	action := flag.String("action", "verify-images", "verify-images, gc-images, backup-lock, backup-unlock, fixture, hold-heartbeat, limiter-probe")
	apply := flag.Bool("apply", false, "apply GC; default is a read-only candidate report")
	grace := flag.Duration("grace", 24*time.Hour, "GC minimum unreferenced age (at least 24h)")
	owner := flag.String("owner", "", "unique backup lease owner")
	hold := flag.Duration("hold", 150*time.Second, "isolated heartbeat lock duration")
	flag.Parse()
	if *action == "limiter-probe" {
		return probe()
	}
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := db.OpenMySQL(ctx, cfg.MySQLDSN)
	if err != nil {
		return errors.New("cannot open explicitly configured MySQL")
	}
	defer pool.Close()
	if *action == "fixture" || *action == "hold-heartbeat" {
		return isolatedReliabilityOperation(ctx, pool, &cfg, *action, *hold)
	}
	box, err := securestore.New(cfg.DataKey)
	if err != nil {
		return err
	}
	store, err := blob.NewMySQL(pool.DB)
	if err != nil {
		return err
	}
	manager := &blob.Manager{DB: pool, Store: &blob.Encrypted{Store: store, Box: box}}
	switch *action {
	case "verify-images":
		records, err := manager.VerifyReferences(ctx)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(records)
	case "gc-images":
		if *grace < 24*time.Hour {
			return errors.New("GC grace must be at least 24h")
		}
		var now time.Time
		if err = pool.QueryRowContext(ctx, "SELECT CURRENT_TIMESTAMP(6)").Scan(&now); err != nil {
			return err
		}
		count, err := manager.Collect(ctx, now.Add(-*grace), !*apply)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"dry_run": !*apply, "unreferenced_candidates": count})
	case "backup-lock":
		return manager.AcquireBackup(ctx, *owner, 3600)
	case "backup-unlock":
		return manager.ReleaseBackup(ctx, *owner)
	default:
		return errors.New("unknown action")
	}
}

func fixture(ctx context.Context, pool *db.Database, cfg config.Config) error {
	var tables int
	if err := pool.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema=DATABASE()").Scan(&tables); err != nil {
		return err
	}
	if tables != 0 {
		return errors.New("fixture refuses a nonempty database")
	}
	secret := os.Getenv("LAB_DEVICE_SECRET")
	stateFile := os.Getenv("LAB_STATE_FILE")
	if len(secret) < 24 || len(secret) > 64 || stateFile == "" {
		return errors.New("fixture requires LAB_DEVICE_SECRET (24..64 bytes) and private LAB_STATE_FILE")
	}
	if err := db.MigrateMySQL(ctx, pool); err != nil {
		return err
	}
	hash := sha256.Sum256([]byte("yihe-device-secret-v1\x00" + secret))
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, sql := range []string{
		"INSERT INTO users(user_id,user_name,role,user_phone) VALUES(1,'caregiver','1','13800000011'),(2,'reviewer','1','13800000012')",
		"INSERT INTO family(family_id,family_name,family_phone) VALUES(1,'synthetic','13800000001')",
		"INSERT INTO oldperson(old_id,name,family_id) VALUES(1,'synthetic',1)",
		"INSERT INTO care_staff_permission(admin_id,duty,assigned_by) VALUES(1,'caregiver',2),(2,'supervisor',2)",
	} {
		if _, err = tx.ExecContext(ctx, sql); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO device(device_id,device_no,secret,old_id) VALUES(1,'LAB-WATCH',?,1)", "sha256$"+base64.RawURLEncoding.EncodeToString(hash[:])); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	box, err := securestore.New(cfg.DataKey)
	if err != nil {
		return err
	}
	store := session.NewStore(time.Hour)
	defer store.Close()
	store.UseDatabase(pool.DB, box)
	var accounts []map[string]any
	for _, id := range []int64{1, 2} {
		sess := store.Create(session.Principal{Role: auth.RoleAdmin, ID: id})
		if sess == nil {
			return errors.New("fixture session creation failed")
		}
		accounts = append(accounts, map[string]any{"actor": id, "cookie": sess.ID, "csrf": sess.CSRFToken})
	}
	file, err := os.OpenFile(stateFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = json.NewEncoder(file).Encode(accounts); err != nil {
		return err
	}
	fmt.Println("Disposable synthetic fixture initialized; credentials written only to private state file")
	return nil
}

func probe() error {
	l := ratelimit.New()
	defer l.Close()
	for wave := range 3 {
		var next atomic.Int64
		var workers sync.WaitGroup
		for range 256 {
			workers.Go(func() {
				r := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/api/v1/auth/login"}}
				for {
					n := next.Add(1)
					if n > 100000 {
						return
					}
					id := int64(wave)*100000 + n
					if id%2 == 0 {
						r.RemoteAddr = fmt.Sprintf("[2001:db8::%x]:42", id)
					} else {
						r.RemoteAddr = fmt.Sprintf("198.%d.%d.%d:42", (id>>16)&255, (id>>8)&255, id&255)
					}
					l.Allow(r)
				}
			})
		}
		workers.Wait()
		runtime.GC()
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		json.NewEncoder(os.Stdout).Encode(map[string]any{"scope": "microbenchmark; simulated sources, not HTTP/staging/production", "wave": wave + 1, "cumulative_distinct_sources": (wave + 1) * 100000, "workers": 256, "live_heap_bytes": memory.HeapAlloc, "runtime_reserved_bytes": memory.Sys})
	}
	l.WriteMetrics(os.Stdout)
	time.Sleep(2 * time.Second)
	return nil
}

func isolatedReliabilityOperation(ctx context.Context, pool *db.Database, cfg *config.Config, action string, hold time.Duration) error {
	dsn, err := mysql.ParseDSN(cfg.MySQLDSN)
	if err != nil {
		return err
	}
	if !regexp.MustCompile(`^yihe_drill_[a-z0-9_]+$`).MatchString(dsn.DBName) {
		return errors.New("isolated operations require a yihe_drill_* disposable database")
	}
	if action == "fixture" {
		return fixture(ctx, pool, *cfg)
	}
	if hold < time.Second || hold > 5*time.Minute {
		return errors.New("heartbeat lock must be 1s..5m")
	}
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var singleton bool
	if err = tx.QueryRowContext(ctx, "SELECT singleton FROM care_worker_heartbeat FOR UPDATE").Scan(&singleton); err != nil {
		return err
	}
	fmt.Println("HEARTBEAT_ROW_LOCKED (no task state changed)")
	timer := time.NewTimer(hold)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
