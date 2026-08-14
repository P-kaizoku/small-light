package service

// HOW TO TEST AuthService:
//
// It only depends on the UserStore interface, so a map-backed fake is enough
// — no database, no Redis. The fake below is completed; each test constructs a
// fresh AuthService:
//
//	svc := NewAuthService(fake, []byte("test-secret"), time.Hour)

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/google/uuid"
)

// fakeUserStore is a map keyed by (already-normalized) email plus an error
// hook. It satisfies the UserStore interface.
type fakeUserStore struct {
	users     map[string]*model.User
	createErr error
}

func (f *fakeUserStore) Create(_ context.Context, email, passwordHash string) (*model.User, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	u := &model.User{Email: email, PasswordHash: passwordHash}
	f.users[email] = u
	return u, nil
}

// GetByEmail mirrors the real repository's contract: a missing email surfaces
// as repository.ErrNotFound, which Login must translate to
// ErrInvalidCredentials.
func (f *fakeUserStore) GetByEmail(_ context.Context, email string) (*model.User, error) {
	u, ok := f.users[email]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return u, nil
}

// TestRegisterNormalizesEmail asserts "  Foo@Bar.com " (spaces + mixed case)
// is stored as "foo@bar.com". Required — if normalization broke, logins for
// "Foo@Bar.com" would never match the stored "foo@bar.com".
func TestRegisterNormalizesEmail(t *testing.T) {
	store := &fakeUserStore{users: map[string]*model.User{}}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	u, err := svc.Register(context.Background(), "  Foo@Bar.com ", "password123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if u.Email != "foo@bar.com" {
		t.Errorf("Email = %q, want %q", u.Email, "foo@bar.com")
	}
	if _, ok := store.users["foo@bar.com"]; !ok {
		t.Error("stored email is not normalized")
	}
}

// TestRegisterInvalidEmail asserts a string that fails mail.ParseAddress
// returns ErrInvalidInput. Required — garbage emails must never be persisted.
func TestRegisterInvalidEmail(t *testing.T) {
	store := &fakeUserStore{users: map[string]*model.User{}}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	_, err := svc.Register(context.Background(), "not-an-email", "password123")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

// TestRegisterShortPassword asserts a password shorter than minPasswordLen (8)
// returns ErrInvalidInput. Required — weak passwords are the cheapest attack
// surface on a URL shortener's accounts.
func TestRegisterShortPassword(t *testing.T) {
	store := &fakeUserStore{users: map[string]*model.User{}}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	_, err := svc.Register(context.Background(), "foo@bar.com", "short")
	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

// TestRegisterDuplicateEmail asserts a repository conflict surfaces as the
// domain error ErrDuplicateEmail, not a raw SQL error. Required — handlers
// map ErrDuplicateEmail to 409; leaking the raw error would break that.
func TestRegisterDuplicateEmail(t *testing.T) {
	store := &fakeUserStore{users: map[string]*model.User{}, createErr: repository.ErrConflict}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	_, err := svc.Register(context.Background(), "foo@bar.com", "password123")
	if !errors.Is(err, ErrDuplicateEmail) {
		t.Errorf("err = %v, want ErrDuplicateEmail", err)
	}
}

// TestLoginSuccess asserts correct credentials return the user. Required —
// the whole auth flow hinges on this round-trip.
func TestLoginSuccess(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := &fakeUserStore{users: map[string]*model.User{
		"foo@bar.com": {Email: "foo@bar.com", PasswordHash: hash},
	}}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	u, err := svc.Login(context.Background(), "foo@bar.com", "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if u.Email != "foo@bar.com" {
		t.Errorf("Email = %q, want %q", u.Email, "foo@bar.com")
	}
}

// TestLoginUnknownEmail asserts a missing email returns ErrInvalidCredentials
// (not a "not found" error). Required — the response must never reveal whether
// an email is registered.
func TestLoginUnknownEmail(t *testing.T) {
	store := &fakeUserStore{users: map[string]*model.User{}}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	_, err := svc.Login(context.Background(), "nobody@example.com", "whatever")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
}

// TestLoginWrongPassword asserts a bad password returns ErrInvalidCredentials.
// Required — same non-revealing contract as the unknown-email case.
func TestLoginWrongPassword(t *testing.T) {
	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	store := &fakeUserStore{users: map[string]*model.User{
		"foo@bar.com": {Email: "foo@bar.com", PasswordHash: hash},
	}}
	svc := NewAuthService(store, []byte("test-secret"), time.Hour)

	_, err = svc.Login(context.Background(), "foo@bar.com", "wrongpass")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want ErrInvalidCredentials", err)
	}
}

// TestHashAndVerifyPassword asserts the bcrypt pair works both ways. Required
// — a hash that equals the plaintext would mean no hashing is happening.
func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "secret" {
		t.Error("hash must differ from the plaintext")
	}
	if !VerifyPassword(hash, "secret") {
		t.Error("VerifyPassword = false, want true for the right password")
	}
	if VerifyPassword(hash, "wrong") {
		t.Error("VerifyPassword = true, want false for the wrong password")
	}
}

// TestGenerateParseTokenRoundTrip asserts a minted token parses back to the
// same user id. Required — RequireAuth depends entirely on this round-trip.
func TestGenerateParseTokenRoundTrip(t *testing.T) {
	uid := uuid.New()
	svc := NewAuthService(&fakeUserStore{users: map[string]*model.User{}}, []byte("test-secret"), time.Hour)

	token, err := svc.GenerateToken(uid)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	claims, err := svc.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if claims.UserID != uid {
		t.Errorf("UserID = %v, want %v", claims.UserID, uid)
	}
	if !claims.ExpiresAt.After(time.Now()) {
		t.Error("ExpiresAt is in the past, want future")
	}
}

// TestParseTokenTampered asserts a token with one changed character fails
// signature verification. Required — if tampering passed, sessions would be
// forgeable.
func TestParseTokenTampered(t *testing.T) {
	svc := NewAuthService(&fakeUserStore{users: map[string]*model.User{}}, []byte("test-secret"), time.Hour)

	token, err := svc.GenerateToken(uuid.New())
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}

	tampered := token[:len(token)-2] + "x" + token[len(token)-1:]
	if _, err := svc.ParseToken(tampered); err == nil {
		t.Error("ParseToken(tampered) = nil, want signature error")
	}
}
