// Package handlers implements the application HTTP endpoints.
package handlers

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"yihe-care/internal/blob"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/ratelimit"
	"yihe-care/internal/risk"

	"yihe-care/internal/config"
	"yihe-care/internal/securestore"
	"yihe-care/internal/session"
	"yihe-care/internal/telemetryworkflow"
)

// Handler 聚合依赖，各组 endpoint 实现在 handler_*.go 中。
type Handler struct {
	DB           *dbstore.Database
	Sessions     *session.Store
	Cfg          config.Config
	Box          *securestore.Box
	CookieSecure bool // 非回环监听时会话 Cookie 携带 Secure
	healthCache  *statisticsCache
	healthWrites healthWriteAdmission
	imageOnce    sync.Once
	imageManager *blob.Manager
	imageError   error
	clientIP     func(*http.Request) string
	Risk         *risk.Manager
	Heartbeats   *telemetryworkflow.HeartbeatBuffer
}

func New(pool *dbstore.Database, sessions *session.Store, cfg config.Config) *Handler {
	return NewWithRisk(pool, sessions, cfg, nil)
}

// NewWithRisk keeps the risk manager instance-owned; New creates a fresh one.
func NewWithRisk(pool *dbstore.Database, sessions *session.Store, cfg config.Config, mgr *risk.Manager) *Handler {
	var box *securestore.Box
	if strings.TrimSpace(cfg.DataKey) != "" {
		var err error
		box, err = securestore.New(cfg.DataKey)
		if err != nil {
			// A live DB handler must never silently downgrade encrypted writes.
			if pool != nil {
				panic(fmt.Errorf("invalid handler encryption configuration: %w", err))
			}
			slog.Error("local_bootstrap_invalid_encryption_key", "error", err)
		}
	} else if pool != nil && cfg.MySQLDSN != "" {
		panic("database handler requires an encryption key")
	}
	resolve, err := ratelimit.NewClientIPResolver(cfg.RateLimit.TrustedProxyCIDRs)
	if err != nil {
		if pool != nil {
			panic(fmt.Errorf("invalid trusted proxy configuration: %w", err))
		}
		slog.Warn("local_bootstrap_invalid_proxy_config", "error", err)
		resolve, _ = ratelimit.NewClientIPResolver(nil)
	}
	if mgr == nil {
		mgr = risk.New()
	}
	return &Handler{DB: pool, Sessions: sessions, Cfg: cfg, Box: box, CookieSecure: cfg.SecureCookies(), healthCache: newHealthStatisticsCache(cfg.HealthCacheBytes), healthWrites: newHealthWriteAdmission(), clientIP: resolve, Risk: mgr}
}

// Routes 注册全部路由。各组在自己的文件中通过 h.route 注册。
func (h *Handler) Routes(mux *http.ServeMux) {
	h.routeAuth(mux) // 登录/session/setup/desktop
	h.routeRisk(mux)
	h.routeUser(mux)            // 用户/客户
	h.routeFamily(mux)          // 家属/邀请码
	h.routeResidentDevices(mux) // 老人/设备/位置/告警处置
	h.routeVitals(mux)          // 心率血压/步数睡眠
	h.routeSocial(mux)          // 说说/文章/图片展示/图片接口
	h.routeHealth(mux)
	h.routeCare(mux) // 照护事件队列、接单及审核状态
	h.routeCapabilities(mux)
	h.routeOperations(mux)
	mux.HandleFunc("POST /api/v1/security/factor", h.securityFactor)
	mux.HandleFunc("POST /api/v1/security/login-audit", h.securityLoginAudit)
	mux.HandleFunc("POST /api/v1/notifications/search", h.notifications)
	mux.HandleFunc("POST /api/v1/rule-policies", h.rulePolicy)
}

// InitRiskWithConfig binds persisted risk state and intelligence to one manager.
func InitRiskWithConfig(ctx context.Context, pool *dbstore.Database, mgr *risk.Manager, cfg config.RiskConfig) error {
	if mgr == nil {
		return fmt.Errorf("risk manager is required")
	}
	if err := mgr.EnableMySQL(pool.DB); err != nil {
		return err
	}
	if path := strings.TrimSpace(cfg.ProxyCIDRsFile); path != "" {
		if err := mgr.LoadProxyCIDRs(path); err != nil {
			log.Printf("risk IP feed unavailable: %v", err)
		}
	}
	if path := strings.TrimSpace(cfg.NetworkFeedPath); path != "" {
		if err := mgr.StartNetworkRefresh(ctx, path, 15*time.Minute, func(err error) {
			log.Printf("risk intelligence update failed: %v", err)
		}); err != nil {
			log.Printf("risk intelligence initial load failed: %v", err)
		}
	}
	return nil
}
