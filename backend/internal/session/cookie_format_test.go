package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCookieIDFormat(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("A", 64), strings.Repeat("g", 64), strings.Repeat("0", 63) + "é"} {
		if validCookieID(id) {
			t.Fatalf("invalid cookie accepted: length=%d", len(id))
		}
	}
	if !validCookieID(strings.Repeat("0123456789abcdef", 4)) {
		t.Fatal("issued cookie format rejected")
	}
}

func TestMalformedCookieDoesNotAccessStorage(t *testing.T) {
	connector := &contextConnector{started: make(chan context.Context, 1)}
	pool, box := contextDatabase(t, connector)
	s := NewStore(time.Hour)
	t.Cleanup(s.Close)
	s.UseDatabase(pool, box)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/test", nil).WithContext(ctx)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "invalid"})
	if s.FromRequest(req) != nil {
		t.Fatal("invalid cookie authenticated")
	}
	select {
	case <-connector.started:
		t.Fatal("invalid cookie accessed storage")
	default:
	}
}
