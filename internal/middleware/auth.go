// Package middleware provides HTTP middleware for the API.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/P-kaizoku/small-light/internal/service"
	"github.com/google/uuid"
)

type ctxKey struct{}

// RequireAuth validates the "Authorization: Bearer <token>" header and, on
// success, stores the user's id in the request context.
func RequireAuth(auth *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Authorization")
			if !strings.HasPrefix(raw, "Bearer ") {
				unauthorized(w)
				return
			}

			claims, err := auth.ParseToken(strings.TrimPrefix(raw, "Bearer "))
			if err != nil {
				unauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), ctxKey{}, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// unauthorized writes the shared 401 response inline (this package cannot use
// handler.writeJSON, which is unexported in another package).
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}

// UserID extracts the authenticated user's id from the context, returning
// false when absent (a route mounted without RequireAuth).
func UserID(ctx context.Context) (uuid.UUID, bool) {
	uid, ok := ctx.Value(ctxKey{}).(uuid.UUID)
	return uid, ok
}
