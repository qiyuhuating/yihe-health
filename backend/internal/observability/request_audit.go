package observability

import (
	"context"
	"log/slog"
	"net/http"
)

type auditActorKey struct{}
type auditActor struct {
	id   int64
	role string
}

// The outer request logger owns this record. Authentication can attach only
// its server-verified principal; the outer logger does not query the store.
func withRequestAudit(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), auditActorKey{}, &auditActor{}))
}

// SetAuthenticatedActor must be called synchronously after session validation.
func SetAuthenticatedActor(r *http.Request, id int64, role string) {
	if a, ok := r.Context().Value(auditActorKey{}).(*auditActor); ok && id > 0 && (role == "ADMIN" || role == "FAMILY") {
		a.id, a.role = id, role
	}
}

func logHTTPRequest(req *http.Request, id, route, method string, status int, durationMS float64, responseBytes int64) {
	var storage [8]slog.Attr
	attrs := append(storage[:0],
		slog.String("request_id", id), slog.String("route", route), slog.String("method", method),
		slog.Int("status", status), slog.Float64("duration_ms", durationMS), slog.Int64("response_bytes", responseBytes))
	if a, ok := req.Context().Value(auditActorKey{}).(*auditActor); ok && a.id > 0 {
		attrs = append(attrs, slog.Int64("actor_id", a.id), slog.String("actor_role", a.role))
	}
	slog.LogAttrs(context.Background(), slog.LevelInfo, "http_request", attrs...)
}
