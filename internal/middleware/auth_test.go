package middleware

// HOW TO TEST RequireAuth:
//
// RequireAuth needs an *service.AuthService to sign and verify tokens. Mint
// real tokens with the service itself so the signature/expiry logic is real:
//
//	svc := service.NewAuthService(noopUserStore{}, []byte("test-secret"), time.Hour)
//	token, _ := svc.GenerateToken(userID)
//	req.Header.Set("Authorization", "Bearer "+token)
//
// noopUserStore is a dummy whose token methods never touch the store — it only
// exists to satisfy the UserStore constructor parameter.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/P-kaizoku/small-light/internal/service"
	"github.com/google/uuid"
)

// noopUserStore satisfies service.UserStore; token minting/parsing never calls
// these methods.
type noopUserStore struct{}

func (noopUserStore) Create(context.Context, string, string) (*model.User, error) {
	return nil, nil
}

func (noopUserStore) GetByEmail(context.Context, string) (*model.User, error) {
	return nil, nil
}

// TestRequireAuthValid asserts a valid Bearer token reaches `next` with the
// user id in context. Required — this is the happy path every protected route
// depends on, and UserID(r.Context()) is what handlers read.
func TestRequireAuthValid(t *testing.T) {
	uid := uuid.New()
	svc := service.NewAuthService(noopUserStore{}, []byte("test-secret"), time.Hour)
	token, err := svc.GenerateToken(uid)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	var gotUserID uuid.UUID
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID, gotOK = UserID(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	RequireAuth(svc)(next).ServeHTTP(rec, req)

	if !gotOK {
		t.Error("UserID: ok = false, want true")
	}
	if gotUserID != uid {
		t.Errorf("UserID = %v, want %v", gotUserID, uid)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestRequireAuthNoHeader asserts a request with no Authorization header gets
// 401 and `next` is NOT called. Required — unauthenticated requests must be
// rejected before any handler runs.
func TestRequireAuthNoHeader(t *testing.T) {
	svc := service.NewAuthService(noopUserStore{}, []byte("test-secret"), time.Hour)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	RequireAuth(svc)(next).ServeHTTP(rec, req)

	if called {
		t.Error("next was called, want rejection")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestRequireAuthBadScheme asserts a non-Bearer scheme (e.g. "Token <x>") is
// rejected with 401 — the Bearer prefix check must be strict.
func TestRequireAuthBadScheme(t *testing.T) {
	svc := service.NewAuthService(noopUserStore{}, []byte("test-secret"), time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Token abc")
	rec := httptest.NewRecorder()
	RequireAuth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next was called, want rejection")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestRequireAuthInvalidToken asserts a malformed/unverifiable token is
// rejected with 401. Required — a garbage token must never be treated as
// authenticated.
func TestRequireAuthInvalidToken(t *testing.T) {
	svc := service.NewAuthService(noopUserStore{}, []byte("test-secret"), time.Hour)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer garbage")
	rec := httptest.NewRecorder()
	RequireAuth(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next was called, want rejection")
	})).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestUserIDAbsent asserts UserID returns ok == false for a context that never
// passed through RequireAuth. Required — a route mounted without the
// middleware must be able to detect "no user" instead of reading a zero id.
func TestUserIDAbsent(t *testing.T) {
	if _, ok := UserID(context.Background()); ok {
		t.Error("ok = true, want false for a bare context")
	}
}
