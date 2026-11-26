package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"yihe-care/internal/blob"
	"sync/atomic"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/config"
	"yihe-care/internal/csrf"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/handlers"
	"yihe-care/internal/observability"
	"yihe-care/internal/ratelimit"
	"yihe-care/internal/response"
	"yihe-care/internal/risk"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
	"yihe-care/internal/telemetryworkflow"
)

// Only fully migrated database handlers are published to HTTP requests.
type databaseService struct {
	api          atomic.Pointer[http.Handler]
	currentDB    atomic.Pointer[dbstore.Database]
	limiter      atomic.Pointer[ratelimit.Limiter]
	desktopToken string
	// configErr 记录启动阶段的配置错误。它一旦非空，watchDatabase 不再进入
	// 重连循环，服务状态接口也会把原因暴露出来，避免前端一直显示"正在自动连接"。
	configErr error
}

func watchDatabase(ctx context.Context, cfg config.Config, sessions *session.Store, service *databaseService) {
	// 启动阶段已校验过配置；这里不再静默退出，而是把原因和后果一起写清楚。
	// 下面的两个 return 保留为纵深防御，正常路径不会走到。
	if service.configErr != nil {
		log.Printf("database API disabled by configuration: %v", service.configErr)
		return
	}
	if cfg.SessionBackend != "" && cfg.SessionBackend != "database" && cfg.SessionBackend != "memory" {
		log.Printf("unsupported SESSION_BACKEND=%q; database API remains unavailable", cfg.SessionBackend)
		return
	}
	var pool *dbstore.Database
	var instanceRisk *risk.Manager
	limiter, limiterErr := ratelimit.NewWithOptions(cfg.RateLimit)
	if limiterErr != nil {
		log.Printf("invalid rate limit configuration: %v", limiterErr)
		return
	}
	service.limiter.Store(limiter)
	defer service.limiter.Store(nil)
	defer limiter.Close()
	var workerDone <-chan struct{}
	defer func() {
		// Drain the care worker before closing its shared connection pool.
		if workerDone != nil {
			<-workerDone
		}
		if instanceRisk != nil {
			if dropped := instanceRisk.CloseMySQL(); dropped > 0 {
				log.Printf("risk audit dropped events: %d", dropped)
			}
		}
		service.currentDB.Store(nil)
		if pool != nil {
			pool.Close()
		}
	}()
	var api http.Handler
	firstAttempt := true
	for {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		var err error
		if pool == nil {
			pool, err = dbstore.OpenMySQL(attempt, cfg.MySQLDSN)
		} else {
			err = pool.PingContext(attempt)
		}
		cancel()
		if err == nil && api == nil {
			migration, cancelMigration := context.WithTimeout(ctx, 2*time.Minute)
			err = dbstore.MigrateMySQL(migration, pool)
			if err == nil {
				var missing int64
				missing, err = blob.MissingMySQLReferences(migration, pool.DB)
				if err == nil && missing != 0 {
					err = fmt.Errorf("%d referenced images require migration to MySQL; use cmd/import-images before starting", missing)
				}
			}
			cancelMigration()
			if err == nil {
				if instanceRisk == nil {
					instanceRisk = risk.New()
				}
				err = handlers.InitRiskWithConfig(ctx, pool, instanceRisk, cfg.Risk)
			}
			if err == nil {
				if cfg.SessionBackend == "database" {
					var box *securestore.Box
					box, err = securestore.New(cfg.DataKey)
					if err != nil {
						log.Printf("database sessions require a valid DATA_ENCRYPTION_KEY: %v", err)
						return
					}
					sessions.UseDatabase(pool.DB, box)
				}
				mux := http.NewServeMux()
				h := handlers.NewWithRisk(pool, sessions, cfg, instanceRisk)
				h.Heartbeats = telemetryworkflow.NewHeartbeatBuffer(pool)
				h.Routes(mux)
				observability.Default.SetRouteResolver(func(r *http.Request) string { _, pattern := mux.Handler(r); return pattern })
				workerDone = startDatabaseWorkers(ctx, databaseWorkerDeps{
					pool:            pool,
					box:             h.Box,
					runCapabilities: h.RunCapabilities,
					heartbeats:      h.Heartbeats,
				})
				api = limiter.Middleware(csrf.Middleware(sessions, auth.Middleware(sessions, h.EnforceAdminDuty(h.EnforceDeviceSignatures(sensitiveCareAudit(pool, response.Router(mux)))))))
			}
		}
		if err == nil {
			service.currentDB.Store(pool)
			observability.Default.Set("yihe_mysql_health_probe_ok", 1)
			if service.api.Load() == nil {
				log.Print("database ready")
			}
			service.api.Store(&api)
		} else {
			observability.Default.Set("yihe_mysql_health_probe_ok", 0)
			observability.Default.Add("yihe_mysql_health_probe_failures_total", 1)
			if service.api.Swap(nil) != nil || firstAttempt {
				log.Printf("database unavailable: %v; reconnecting in background", err)
			}
		}
		firstAttempt = false
		timer := time.NewTimer(3 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
