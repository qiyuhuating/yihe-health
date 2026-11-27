package auth_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"yihe-care/internal/auth"
	"yihe-care/internal/observability"
	"yihe-care/internal/session"
)

func TestRequestAuditUsesVerifiedPrincipal(t *testing.T) {
	var output bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	sess := store.Create(session.Principal{Role: auth.RoleFamily, ID: 42})
	reg := observability.New()
	reg.SetRouteResolver(func(*http.Request) string { return "/api/v1/auth/logout" })
	calls := 0
	h := reg.HTTP(auth.Middleware(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte("ok"))
	})))
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout?secret=private-query", strings.NewReader("private-body"))
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	r.Header.Set("X-Request-ID", "spoofed-request")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["actor_id"] != float64(42) || entry["actor_role"] != auth.RoleFamily || entry["response_bytes"] != float64(2) || entry["status"] != float64(200) || entry["request_id"] != w.Header().Get("X-Request-ID") {
		t.Fatalf("incorrect audit: %v", entry)
	}
	for _, secret := range []string{sess.ID, "private-query", "private-body", "spoofed-request"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("private or untrusted value logged")
		}
	}
	store.Destroy(sess.ID)
	output.Reset()
	r = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	r = session.WithContext(r, sess)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	entry = nil
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if w.Code != 401 || calls != 1 || entry["actor_id"] != nil || entry["actor_role"] != nil || entry["status"] != float64(401) {
		t.Fatalf("revoked session attributed: %v", entry)
	}
}
