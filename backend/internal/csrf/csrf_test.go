package csrf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/response"
	"yihe-care/internal/session"
)

func TestMiddlewareRejectsMissingAndInvalidToken(t *testing.T) {
	store := session.NewStore(time.Hour)
	defer store.Close()

	sess := store.Create(session.Principal{Role: "ADMIN", ID: 1})
	called := false
	handler := Middleware(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Missing session cookie
	req := httptest.NewRequest(http.MethodPost, "/api/v1/residents/list", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("expected 403 without session, got status=%d called=%v", rec.Code, called)
	}
	var body response.BaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if body.Code != "CSRF_REJECTED" || body.ReturnCode != response.CodeFail {
		t.Fatalf("unexpected error payload: %+v", body)
	}

	// 2. Valid session cookie, wrong CSRF token
	req = httptest.NewRequest(http.MethodPost, "/api/v1/residents/list", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	req.Header.Set(HeaderName, "wrong-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("expected 403 with wrong token, got status=%d called=%v", rec.Code, called)
	}
}

func TestMiddlewareAllowsValidTokenAndExemptAndSafeMethods(t *testing.T) {
	store := session.NewStore(time.Hour)
	defer store.Close()

	sess := store.Create(session.Principal{Role: "ADMIN", ID: 7})
	var gotPrincipalID int64
	handler := Middleware(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s := session.FromContext(r); s != nil {
			gotPrincipalID = s.Principal.ID
		}
		w.WriteHeader(http.StatusOK)
	}))

	// 1. Valid POST with matching CSRF token
	req := httptest.NewRequest(http.MethodPost, "/api/v1/residents/list", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	req.Header.Set(HeaderName, sess.CSRFToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotPrincipalID != 7 {
		t.Fatalf("expected 200 with session context, got status=%d id=%d", rec.Code, gotPrincipalID)
	}

	// 2. Safe GET method passes without CSRF header
	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected GET to pass, got %d", rec.Code)
	}

	// 3. Exempt device endpoint passes POST without session
	req = httptest.NewRequest(http.MethodPost, "/api/v1/device/report", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected exempt device route to pass, got %d", rec.Code)
	}
}

func TestPublicAssetsDoNotResolveSession(t *testing.T) {
	store := session.NewStore(time.Hour)
	defer store.Close()
	sess := store.Create(session.Principal{Role: "ADMIN", ID: 7})
	handler := Middleware(store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if session.FromContext(r) != nil {
			t.Fatal("public asset resolved a session")
		}
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/", "/healthz", "/index.html", "/favicon.ico", "/assets/index.js", "/static/logo.png"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			req := httptest.NewRequest(method, path, nil)
			req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatal(path, method, rec.Code)
			}
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/assets/index.js", nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: sess.ID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("unsafe method bypassed CSRF", rec.Code)
	}
}
