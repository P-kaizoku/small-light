package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// requestIDKey is the context key for the request-scoped request id.
//
// Note: auth.go already uses its own unexported ctxKey{} for the authenticated
// user id. If request-scoped values keep multiplying, centralize them in one
// place later.
type requestIDKey struct{}

// RequestID assigns every request a request id and echoes it back via the
// X-Request-ID response header. The id is stored in the context so downstream
// code (RequestLogger, Recover) can attach it to their log lines — that is the
// "context propagation" pattern: request-scoped data rides along in the
// context instead of being threaded through every function signature.
//
// An incoming X-Request-ID header (set by a load balancer, or by a client that
// wants to correlate its own requests) is honored as-is; otherwise a fresh
// UUID is generated.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}

		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		w.Header().Set("X-Request-ID", id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFrom extracts the request id set by RequestID. It returns "" when
// the request did not flow through that middleware (e.g. a test calling a
// handler directly), so callers can log it unconditionally.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}
