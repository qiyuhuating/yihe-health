package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/handlers"
	"yihe-care/internal/session"
)

func TestConfigurationErrorIsVisibleWithoutBlockingApplication(t *testing.T) {
	cfg := config.Config{SessionBackend: "database", DataKey: "invalid-key-must-not-be-disclosed"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("invalid database session key accepted")
	}
	for _, test := range []struct {
		name    string
		service *databaseService
		want    string
	}{
		{"connection outage", &databaseService{}, ""},
		// /api/v1/service/status 公开可读，只回固定文案，不透出具体配置细节。
		{"configuration error", &databaseService{configErr: err},
			"服务端配置有误，依赖数据库的功能不可用，自动重连不会恢复；请检查环境变量配置后重启服务"},
	} {
		t.Run(test.name, func(t *testing.T) {
			local := http.NewServeMux()
			local.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("application")) })
			app := test.service.middleware(local)
			status := httptest.NewRecorder()
			app.ServeHTTP(status, httptest.NewRequest("GET", "/api/v1/service/status", nil))
			var body struct {
				DatabaseReady bool   `json:"databaseReady"`
				ConfigError   string `json:"configError"`
			}
			if err := json.Unmarshal(status.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if status.Code != http.StatusOK || body.DatabaseReady || body.ConfigError != test.want {
				t.Fatalf("unexpected status: HTTP %d %s", status.Code, status.Body.String())
			}
			if strings.Contains(status.Body.String(), cfg.DataKey) || strings.Contains(status.Body.String(), err.Error()) {
				t.Fatal("configuration diagnostic disclosed the key or raw error detail")
			}
			page := httptest.NewRecorder()
			app.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
			if page.Code != http.StatusOK || page.Body.String() != "application" {
				t.Fatal("configuration error blocked the local application")
			}
			business := httptest.NewRecorder()
			app.ServeHTTP(business, httptest.NewRequest("POST", "/api/v1/device/sos", nil))
			if business.Code != http.StatusServiceUnavailable || !strings.Contains(business.Body.String(), "DATABASE_UNAVAILABLE") {
				t.Fatal("unavailable business request did not retain its explicit failure")
			}
		})
	}
}

func TestDatabaseUnavailableDoesNotBlockApplication(t *testing.T) {
	service := &databaseService{}
	local := http.NewServeMux()
	local.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("application")) })
	handlers.New(nil, nil, config.Config{DesktopToken: "test-only-token"}).MountDesktopExit(local)
	app := service.middleware(local)
	for _, test := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/", 200, "application"},
		{"GET", "/api/v1/service/status", 200, `"databaseReady":false`},
		{"GET", "/openapi.json", 401, "需要授权"},
		{"GET", "/api/v1/auth/session", 503, "DATABASE_UNAVAILABLE"},
		{"POST", "/api/v1/device/sos", 503, "DATABASE_UNAVAILABLE"},
		{"POST", "/api/v1/desktop/exit", 403, ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest(test.method, test.path, nil))
			if w.Code != test.status || !strings.Contains(w.Body.String(), test.body) {
				t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
			}
		})
	}
	// Recovery publishes a fully prepared API; a later outage withdraws it again.
	calls := 0
	var api http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) })
	service.api.Store(&api)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/test", nil))
	if w.Code != 204 || calls != 1 {
		t.Fatal("database recovery did not activate API")
	}
	service.api.Store(nil)
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/test", nil))
	if w.Code != 503 || calls != 1 {
		t.Fatal("unavailable database accepted a business operation")
	}
}

func TestLogoutDoesNotRequireDatabase(t *testing.T) {
	store := session.NewStore(12 * time.Hour)
	t.Cleanup(store.Close)
	account := store.Create(session.Principal{Role: "ADMIN", ID: 1})
	local := http.NewServeMux()
	handlers.New(nil, store, config.Config{}).MountLocalLogout(local)
	app := (&databaseService{}).middleware(local)
	request := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: session.CookieName, Value: account.ID})
	request.Header.Set("X-CSRF-TOKEN", account.CSRFToken)
	result := httptest.NewRecorder()
	rejected := httptest.NewRecorder()
	missingCSRF := request.Clone(request.Context())
	missingCSRF.Header = request.Header.Clone()
	missingCSRF.Header.Del("X-CSRF-TOKEN")
	app.ServeHTTP(rejected, missingCSRF)
	if rejected.Code != http.StatusForbidden || store.FromRequest(request) == nil {
		t.Fatal("offline logout weakened CSRF enforcement")
	}
	app.ServeHTTP(result, request)
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), "1111") {
		t.Fatalf("offline logout: HTTP %d %s", result.Code, result.Body.String())
	}
	if store.Get(account.ID) != nil {
		t.Fatal("logout retained the authenticated server session")
	}
}
