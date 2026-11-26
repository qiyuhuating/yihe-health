package auth_test

import (
	"net/http"
	"net/http/httptest"
	"yihe-care/internal/auth"
	"yihe-care/internal/session"
	"testing"
	"time"
)

func TestLogoutRoleAccess(t *testing.T) {
	for _, role := range []string{auth.RoleAdmin, auth.RoleFamily} {
		t.Run(role, func(t *testing.T) {
			store := session.NewStore(time.Hour)
			t.Cleanup(store.Close)
			sess := store.Create(session.Principal{Role: role, ID: 1})
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
			request.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
			response := httptest.NewRecorder()
			auth.Middleware(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(response, request)
			if response.Code != 204 {
				t.Fatalf("%s logout denied: HTTP %d %s", role, response.Code, response.Body.String())
			}
		})
	}
}

func TestContextPrincipalDoesNotReplaceSessionValidation(t *testing.T) {
	store := session.NewStore(time.Hour)
	t.Cleanup(store.Close)
	sess := store.Create(session.Principal{Role: auth.RoleAdmin, ID: 1})
	store.Destroy(sess.ID)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	r = session.WithContext(r, sess)
	w := httptest.NewRecorder()
	auth.Middleware(store, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unvalidated context principal authorized")
	})).ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
