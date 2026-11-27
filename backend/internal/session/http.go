package session

import (
	"context"
	"net/http"
	"time"
)

// SetCookie 写 YIHE_SESSION cookie。
func SetCookie(w http.ResponseWriter, id string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
}

// ClearCookie 清除 YIHE_SESSION cookie。
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
	})
}

type requestKey struct{ store *Store }

type requestMiss struct {
	id  string
	err error
}

// FromRequest 从请求 cookie 取会话；同一请求内复用已验证结果。
// Like gorilla/sessions' request registry, this context is local to the request:
// https://github.com/gorilla/sessions/blob/main/sessions.go
// A new request always reloads storage, so replica revocations remain visible.
func (s *Store) FromRequest(r *http.Request) *Session {
	sess, _ := s.LookupRequest(r)
	return sess
}

// LookupRequest shares the complete verification result within one request,
// including misses and storage failures. New requests always consult the
// authoritative store; a changed cookie or store never reuses the result.
func (s *Store) LookupRequest(r *http.Request) (*Session, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	c, err := r.Cookie(CookieName)
	if err != nil || !validCookieID(c.Value) {
		return nil, nil
	}
	key := requestKey{s}
	switch result := r.Context().Value(key).(type) {
	case *Session:
		if result.ID == c.Value {
			if time.Now().After(result.ExpiresAt) {
				return nil, nil
			}
			return result, nil
		}
	case requestMiss:
		if result.id == c.Value {
			return nil, result.err
		}
	}
	sess, err := s.lookup(r.Context(), c.Value)
	// Preserve the existing allocation profile for valid sessions. Only
	// an unsuccessful lookup needs a separate cookie-bound result.
	var cached any = sess
	if sess == nil {
		cached = requestMiss{c.Value, err}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), key, cached))
	return sess, err
}

// All issued IDs are exactly 32 random bytes encoded as lowercase hex.
// Reject malformed cookies before a database lookup or request cache check.
func validCookieID(id string) bool {
	if len(id) != 64 {
		return false
	}
	for i := range id {
		c := id[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// contextKey 会话在 request context 中的 key。
type contextKey struct{}

// WithContext 把会话放入 request context。
func WithContext(r *http.Request, sess *Session) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), contextKey{}, sess))
}

// FromContext 从 request context 取会话，无则返回 nil。
func FromContext(r *http.Request) *Session {
	sess, _ := r.Context().Value(contextKey{}).(*Session)
	return sess
}
