package config

import (
	"bytes"
	"encoding/base64"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeDefaults(t *testing.T) {
	t.Setenv("MYSQL_DSN", "")
	t.Setenv("PG_ANALYTICS_URL", "")
	t.Setenv("DATABASE_URL", "postgres://legacy.invalid/ignored")
	t.Setenv("ADDR", "")
	cfg := Load()
	if cfg.MySQLDSN != "" || cfg.LegacyPGURL != "" {
		t.Fatal("default credentials must never be embedded in the service")
	}
	if cfg.Addr != "127.0.0.1:8080" {
		t.Fatalf("HTTP service must default to loopback: %q", cfg.Addr)
	}
}

func TestExplicitDeploymentConfiguration(t *testing.T) {
	t.Setenv("MYSQL_DSN", "yihe@tcp(mysql.local:3306)/yihe?tls=true")
	t.Setenv("PG_ANALYTICS_URL", "postgres://example.local/analytics?sslmode=require")
	t.Setenv("ADDR", "0.0.0.0:9090")
	cfg := Load()
	if cfg.MySQLDSN != "yihe@tcp(mysql.local:3306)/yihe?tls=true" || cfg.LegacyPGURL != "postgres://example.local/analytics?sslmode=require" || cfg.Addr != "0.0.0.0:9090" {
		t.Fatal("explicit dual-database deployment configuration ignored")
	}
}

func TestHealthCacheBudget(t *testing.T) {
	t.Setenv("HEALTH_HOT_CACHE_MIB_FILE", "")
	for _, test := range []struct {
		value string
		mib   int64
	}{{"", 16}, {"0", 0}, {"1", 1}, {"256", 256}, {"-1", 16}, {"257", 16}, {"invalid", 16}, {"999999999999999999999", 16}} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("HEALTH_HOT_CACHE_MIB", test.value)
			if got := Load().HealthCacheBytes; got != test.mib<<20 {
				t.Fatalf("cache budget=%d, want %d MiB", got, test.mib)
			}
		})
	}
}

// Validate 存在的意义：把"进程起来了但业务接口永久 503"的配置挡在启动阶段。
// 这些配置错误不会随重试恢复，所以不能只靠 watchDatabase 的 3 秒循环兜底。
func TestValidateRejectsUnusableConfigurations(t *testing.T) {
	validKey := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	for _, test := range []struct {
		name    string
		backend string
		key     string
		dsn     string
		wantErr bool
	}{
		{"memory 模式允许无密钥", "memory", "", "", false},
		{"空后端按单实例处理", "", "", "", false},
		{"database 缺密钥必须报错", "database", "", "", true},
		{"database 空白密钥必须报错", "database", "   ", "", true},
		{"database 非法 base64 必须报错", "database", "not-base64!!", "", true},
		{"database 长度不足必须报错", "database", base64.StdEncoding.EncodeToString([]byte("short")), "", true},
		{"database 合法密钥通过", "database", validKey, "", false},
		{"未知后端必须报错", "postgres", validKey, "", true},
		{"大小写敏感的后端必须报错", "Database", validKey, "", true},
		{"配置 DSN 缺密钥必须报错（memory 后端也一样）", "memory", "", "yihe@tcp(mysql.local:3306)/yihe", true},
		{"配置 DSN 非法密钥必须报错", "memory", "not-base64!!", "yihe@tcp(mysql.local:3306)/yihe", true},
		{"配置 DSN 且密钥合法通过", "memory", validKey, "yihe@tcp(mysql.local:3306)/yihe", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := Config{SessionBackend: test.backend, DataKey: test.key, MySQLDSN: test.dsn}
			err := cfg.Validate()
			if test.wantErr && err == nil {
				t.Fatal("配置未被拒绝，会导致业务接口永久 503 且无提示")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("合法配置被拒绝: %v", err)
			}
		})
	}
}

func TestSecureCookies(t *testing.T) {
	for _, test := range []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", false},
		{"localhost:80", false},
		{"[::1]:8080", false},
		{"[::ffff:127.0.0.1]:8080", false},
		{"0.0.0.0:9090", true},
		{"192.168.1.5:8080", true},
		{"example.local:8080", true},
	} {
		if got := (Config{Addr: test.addr}).SecureCookies(); got != test.want {
			t.Fatalf("SecureCookies(%q)=%v, want %v", test.addr, got, test.want)
		}
	}
}

func TestSessionTTLRejectsDurationOverflow(t *testing.T) {
	t.Setenv("SESSION_TTL_HOURS_FILE", "")
	for _, test := range []struct {
		value string
		want  time.Duration
	}{
		{"24", 24 * time.Hour},
		{"3000000", 12 * time.Hour},
		{"-1", 12 * time.Hour},
		{"invalid", 12 * time.Hour},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("SESSION_TTL_HOURS", test.value)
			if got := Load().SessionTTL; got != test.want {
				t.Fatalf("SessionTTL=%s, want %s", got, test.want)
			}
		})
	}
}

func TestUnreadableSecretFileIsReportedWithoutFallback(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	t.Setenv("YIHE_DESKTOP_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("YIHE_DESKTOP_TOKEN", "plaintext-should-not-fallback")
	if got := Secret("YIHE_DESKTOP_TOKEN"); got != "" {
		t.Fatalf("unreadable secret file unexpectedly fell back to environment: %q", got)
	}
	if output := logs.String(); !strings.Contains(output, "configuration_file_read_failed") || !strings.Contains(output, "YIHE_DESKTOP_TOKEN_FILE") || strings.Contains(output, "plaintext-should-not-fallback") {
		t.Fatalf("missing or sensitive config diagnostic: %s", output)
	}
}

func TestDesktopTokenUsesSingleCurrentEnvironmentKey(t *testing.T) {
	t.Setenv("DESKTOP_TOKEN", "obsolete-value")
	t.Setenv("YIHE_DESKTOP_TOKEN", "")
	t.Setenv("YIHE_DESKTOP_TOKEN_FILE", "")
	if token := Load().DesktopToken; token != "" {
		t.Fatalf("obsolete desktop token environment key accepted: %q", token)
	}
	t.Setenv("YIHE_DESKTOP_TOKEN", "current-value")
	if token := Load().DesktopToken; token != "current-value" {
		t.Fatalf("current desktop token not loaded: %q", token)
	}
}

func TestMySQLOnlyStorageConfiguration(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32))
	for _, tt := range []struct {
		name, backend, pgURL string
		allowed              bool
	}{
		{"mysql", "mysql", "", true},
		{"default", "", "", true},
		{"local rejected", "local", "", false},
		{"s3 rejected", "s3", "", false},
		{"other backend rejected", "sqlite", "", false},
		{"pg projection rejected", "mysql", "postgres://host/analytics", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{MySQLDSN: "yihe@tcp(localhost:3306)/yihe", DataKey: key, BlobBackend: tt.backend, LegacyPGURL: tt.pgURL, SessionBackend: "database"}
			err := cfg.Validate()
			if (err == nil) != tt.allowed {
				t.Fatalf("Validate() error = %v, allowed=%v", err, tt.allowed)
			}
		})
	}
	t.Setenv("BLOB_BACKEND", "")
	t.Setenv("PG_ANALYTICS_URL", "")
	t.Setenv("PG_ANALYTICS_URL_FILE", "")
	if cfg := Load(); cfg.BlobBackend != "mysql" || cfg.LegacyPGURL != "" {
		t.Fatalf("MySQL-only defaults not selected: backend=%q", cfg.BlobBackend)
	}
}

func TestConfiguredReceiptSecretFileFailsClosed(t *testing.T) {
	t.Setenv("CARE_RECEIPT_SECRET_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("CARE_RECEIPT_SECRET", "fallback-must-not-apply")
	if err := (Config{SessionBackend: "memory"}).Validate(); err == nil || !strings.Contains(err.Error(), "CARE_RECEIPT_SECRET_FILE") {
		t.Fatalf("missing configured secret file was not rejected: %v", err)
	}
}

func TestConfiguredNotificationSecretFileIsLoaded(t *testing.T) {
	key := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(key, []byte("real-file-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOTIFY_WEBHOOK_TOKEN_FILE", key)
	t.Setenv("NOTIFY_WEBHOOK_TOKEN", "ignored-fallback-token")
	if got := Secret("NOTIFY_WEBHOOK_TOKEN"); got != "real-file-token" {
		t.Fatalf("did not honor the file-backed token")
	}
	if err := ValidateSecretFiles(); err != nil {
		t.Fatal(err)
	}
}
