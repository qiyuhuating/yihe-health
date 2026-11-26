// Package config 从环境变量装配服务配置，连接凭据必须显式配置。
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"yihe-care/internal/ratelimit"
	"yihe-care/internal/securestore"
)

// RiskConfig is bound at startup and passed to an instance-owned risk manager.
type RiskConfig struct {
	ProxyCIDRsFile  string
	NetworkFeedPath string
}

// RequestConfig snapshots request hot-path toggles and HMAC secrets at startup.
// The zero value leaves direct test-constructed handlers with their existing
// environment-injection behavior; production always calls Load().
type RequestConfig struct {
	Loaded            bool
	RequireSigned     bool
	DurableTelemetry  bool
	HealthLateLock    bool
	CareReceiptSecret string
}

type Config struct {
	Addr             string // 默认只监听回环地址，远程部署必须显式配置
	MySQLDSN         string
	LegacyPGURL      string        // Refuse legacy projection configuration until it is migrated
	SessionTTL       time.Duration // 登录 session 有效期，默认 12h
	DesktopToken     string        // X-Yihe-Desktop 桌面端 token
	DataKey          string
	SessionBackend   string
	HealthCacheBytes int64 // 健康统计内存缓存预算；0 禁用
	RateLimit        ratelimit.Options
	BlobBackend      string
	Risk             RiskConfig
	Requests         RequestConfig
}

func getenv(key, def string) string {
	if path := os.Getenv(key + "_FILE"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			slog.Error("configuration_file_read_failed", "env_key", key+"_FILE", "error", err)
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
func Secret(key string) string { return getenv(key, "") }

// Setting is the single environment reader for optional runtime settings.
// Sensitive credentials must use Secret so *_FILE takes precedence.
func Setting(key string) string { return getenv(key, "") }

func Enabled(key string) bool { return strings.TrimSpace(Setting(key)) == "1" }

// ValidateSecretFiles prevents a broken *_FILE reference from silently
// disabling a configured notification, care receipt, or API credential.
func ValidateSecretFiles() error {
	for _, key := range []string{
		"DATA_ENCRYPTION_KEY", "MYSQL_DSN", "YIHE_DESKTOP_TOKEN",
		"CARE_RECEIPT_SECRET", "CARE_NOTIFY_WEBHOOK_TOKEN",
		"NOTIFY_WEBHOOK_TOKEN", "TENCENT_SMS_APPKEY",
		"SECURITY_ALERT_WEBHOOK_TOKEN",
	} {
		path := os.Getenv(key + "_FILE")
		if path == "" {
			continue
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read %s_FILE: %w", key, err)
		}
		if strings.TrimSpace(string(value)) == "" {
			return fmt.Errorf("%s_FILE is empty", key)
		}
	}
	return nil
}

// Validate 拒绝那些"进程能起来、但业务接口永远不可用且不会自愈"的配置。
//
// 这些错误原先发生在 cmd/server 的 watchDatabase 协程里：只打印一行日志就
// return，不再进入 3 秒重连循环；而就绪文件和 /healthz 早已成功，桌面端与
// 探针都认为服务正常。配置错误本身不会随等待消失（环境变量在进程生命周期内
// 不变），所以必须在启动阶段就说清楚，而不是让调用方永远看到"稍后重试"。
func (c Config) Validate() error {
	if err := ValidateSecretFiles(); err != nil {
		return err
	}
	if c.BlobBackend != "" && c.BlobBackend != "mysql" {
		return errors.New("BLOB_BACKEND must be mysql; migrate legacy local/S3 objects before upgrading")
	}
	if strings.TrimSpace(c.LegacyPGURL) != "" {
		return errors.New("PG_ANALYTICS_URL is retired; export any needed projection history and remove this setting")
	}
	if err := ratelimit.ValidateOptions(c.RateLimit); err != nil {
		return err
	}
	// 只要配置了 MySQL，就必须有数据加密密钥：缺少它时照护备注、通知收件人等
	// 敏感字段会明文落库，登录审计退化为可离线穷举的裸哈希——无论会话后端是什么。
	if strings.TrimSpace(c.MySQLDSN) != "" {
		if strings.TrimSpace(c.DataKey) == "" {
			return errors.New("配置 MYSQL_DSN 时必须设置 DATA_ENCRYPTION_KEY（32 字节随机数的 base64）；" +
				"缺少它时敏感字段会明文落库，业务接口会永久返回 503")
		}
		if _, err := securestore.New(c.DataKey); err != nil {
			return fmt.Errorf("DATA_ENCRYPTION_KEY 无效：%w", err)
		}
	}
	if t := strings.TrimSpace(c.DesktopToken); t != "" && len(t) < 24 {
		return errors.New("YIHE_DESKTOP_TOKEN 至少需要 24 个字符（桌面端每次启动自动生成 64 位随机值）；" +
			"过短的令牌可被线速穷举，校验失败时 setup 与 desktop/exit 均会拒绝")
	}
	switch c.SessionBackend {
	case "", "memory":
		// 空值与 memory 都是单实例开发模式，会话保存在进程内存里。
		return nil
	case "database":
	default:
		return fmt.Errorf("SESSION_BACKEND=%q 无效，只能是 database 或 memory", c.SessionBackend)
	}
	if strings.TrimSpace(c.DataKey) == "" {
		return errors.New("SESSION_BACKEND=database 时必须设置 DATA_ENCRYPTION_KEY（32 字节随机数的 base64）；" +
			"缺少它时数据库会话无法加密，业务接口会永久返回 503")
	}
	if _, err := securestore.New(c.DataKey); err != nil {
		return fmt.Errorf("DATA_ENCRYPTION_KEY 无效：%w", err)
	}
	return nil
}

// SecureCookies 报告会话 Cookie 是否应携带 Secure 标志：凡是非回环监听地址，
// 都假定可能经网络（含明文代理）访问，Cookie 必须只在 TLS 上传输。
func (c Config) SecureCookies() bool {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		host = c.Addr
	}
	host = strings.Trim(host, "[]")
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	switch strings.ToLower(host) {
	case "", "localhost", "127.0.0.1", "::1":
		return false
	}
	return true
}

func Load() Config {
	maxKeys, keyErr := strconv.Atoi(getenv("RATE_LIMIT_MAX_KEYS", "8192"))
	maxSlots, slotErr := strconv.Atoi(getenv("RATE_LIMIT_MAX_SLOTS", "262144"))
	if keyErr != nil {
		maxKeys = -1
	}
	if slotErr != nil {
		maxSlots = -1
	}
	proxies := strings.Split(getenv("TRUSTED_PROXY_CIDRS", ""), ",")
	for i := range proxies {
		proxies[i] = strings.TrimSpace(proxies[i])
	}
	ttlHours, err := strconv.Atoi(getenv("SESSION_TTL_HOURS", "12"))
	if err != nil || ttlHours <= 0 || int64(ttlHours) > (1<<63-1)/int64(time.Hour) {
		ttlHours = 12
	}
	cacheMiB, err := strconv.ParseInt(getenv("HEALTH_HOT_CACHE_MIB", "16"), 10, 64)
	if err != nil || cacheMiB < 0 || cacheMiB > 256 {
		cacheMiB = 16
	}
	return Config{
		MySQLDSN:         getenv("MYSQL_DSN", ""),
		LegacyPGURL:      getenv("PG_ANALYTICS_URL", ""),
		Addr:             getenv("ADDR", "127.0.0.1:8080"),
		SessionTTL:       time.Duration(ttlHours) * time.Hour,
		DesktopToken:     getenv("YIHE_DESKTOP_TOKEN", ""),
		DataKey:          getenv("DATA_ENCRYPTION_KEY", ""),
		SessionBackend:   strings.ToLower(strings.TrimSpace(getenv("SESSION_BACKEND", "database"))),
		HealthCacheBytes: cacheMiB << 20,
		RateLimit:        ratelimit.Options{MaxKeys: maxKeys, MaxSlots: maxSlots, TrustedProxyCIDRs: proxies},
		BlobBackend:      strings.ToLower(strings.TrimSpace(getenv("BLOB_BACKEND", "mysql"))),
		Requests: RequestConfig{
			Loaded:            true,
			RequireSigned:     Enabled("YIHE_REQUIRE_SIGNED_DEVICE"),
			DurableTelemetry:  Enabled("YIHE_DURABLE_TELEMETRY"),
			HealthLateLock:    Enabled("HEALTH_LATE_DEVICE_LOCK"),
			CareReceiptSecret: Secret("CARE_RECEIPT_SECRET"),
		},
		Risk: RiskConfig{
			ProxyCIDRsFile:  getenv("YIHE_RISK_PROXY_CIDRS_FILE", ""),
			NetworkFeedPath: getenv("YIHE_RISK_NETWORK_FEED", ""),
		},
	}
}
