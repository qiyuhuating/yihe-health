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

// FromRequest 从请求 cookie 取会话；同一请求内复用已验证结果。
// Like gorilla/sessions' request registry, this context is local to the request:
// https://github.com/gorilla/sessions/blob/main/sessions.go
// A new request always reloads storage, so replica revocations remain visible.
func (s *Store) FromRequest(r *http.Request) *Session {
	if r.Context().Err() != nil {
		return nil
	}
	c, err := r.Cookie(CookieName)
	if err != nil || !validCookieID(c.Value) {
		return nil
	}
	key := requestKey{s}
	if sess, ok := r.Context().Value(key).(*Session); ok && sess.ID == c.Value && !time.Now().After(sess.ExpiresAt) {
		return sess
	}
	sess := s.GetContext(r.Context(), c.Value)
	if sess != nil {
		*r = *r.WithContext(context.WithValue(r.Context(), key, sess))
	}
	return sess
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
