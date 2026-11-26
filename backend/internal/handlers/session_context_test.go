package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yihe-care/internal/config"
	"yihe-care/internal/session"
)

// The same fixture runs against preview@50b9cc0 to reproduce the writes that
// previously ignored a request cancelled before its session operation began.
func TestCancelledSessionWritesDoNotClaimSuccess(t *testing.T) {
	for _, action := range []string{"logout", "renew"} {
		t.Run(action, func(t *testing.T) {
			store := session.NewStore(time.Hour)
			if closer, ok := any(store).(interface{ Close() }); ok {
				t.Cleanup(closer.Close)
			}
			account := store.Create(session.Principal{Role: "ADMIN", ID: 1})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := httptest.NewRequest("POST", "/api/v1/userLogin/"+action, nil).WithContext(ctx)
			r.AddCookie(&http.Cookie{Name: session.CookieName, Value: account.ID})
			r = session.WithContext(r, account)
			w := httptest.NewRecorder()
			h := New(nil, store, config.Config{})
			if action == "logout" {
				h.logout(w, r)
			} else if h.renewSession(w, r, account.Principal) {
				t.Error("cancelled renewal reported success")
			}
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Set-Cookie") != "" || store.Get(account.ID) != account {
				t.Fatalf("cancelled %s changed the session or claimed success: HTTP %d cookie_present=%v", action, w.Code, w.Header().Get("Set-Cookie") != "")
			}
		})
	}
}
