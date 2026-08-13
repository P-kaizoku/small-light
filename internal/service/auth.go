// Package service holds the domain logic. It sits between handlers (HTTP) and
// repositories (SQL): it knows how to hash passwords, mint and verify JWTs,
// validate URLs, and translate repository errors into domain errors.
package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/P-kaizoku/small-light/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// UserStore is the persistence surface AuthService needs. It is defined here
// (at the consumer) so AuthService can be tested against a fake that never
// touches a real database. *repository.UserRepository satisfies it.
type UserStore interface {
	Create(ctx context.Context, email, passwordHash string) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
}

const (
	jwtIssuer      = "small-light"
	minPasswordLen = 8
)

// Claims is what goes inside a signed JWT.
type Claims struct {
	UserID uuid.UUID `json:"user_id"`
	jwt.RegisteredClaims
}

type AuthService struct {
	users  UserStore
	secret []byte
	ttl    time.Duration
}

func NewAuthService(users UserStore, secret []byte, ttl time.Duration) *AuthService {
	return &AuthService{users: users, secret: secret, ttl: ttl}
}

// HashPassword returns a bcrypt hash of password.
func HashPassword(password string) (string, error) {
	hashPass, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(hashPass), nil
}

// VerifyPassword reports whether password matches a bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken mints a signed HS256 JWT for the given user. The claims carry
// the user id plus the registered fields: issuer, subject, issued-at, expiry.
func (s *AuthService) GenerateToken(userID uuid.UUID) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    jwtIssuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", err
	}
	return token, nil
}

// ParseToken validates signature, expiry and issuer, and returns the claims.
func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	var claims Claims
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(*jwt.Token) (any, error) {
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	return &claims, nil
}

// Register creates a user and returns it. The email is normalized (lowercased,
// trimmed) before validation, and a duplicate email surfaces as
// ErrDuplicateEmail instead of the repository's raw conflict.
func (s *AuthService) Register(ctx context.Context, email, password string) (*model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	_, err := mail.ParseAddress(email)
	if err != nil || len(password) < minPasswordLen {
		return nil, ErrInvalidInput
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	user, err := s.users.Create(ctx, email, hash)

	if err != nil {
		if errors.Is(err, repository.ErrConflict) {
			return nil, ErrDuplicateEmail
		}
		return nil, err
	}

	return user, nil
}

// Login verifies credentials and returns the user on success.
//
// The email is normalized exactly like Register does, otherwise logins for
// "Foo@Bar.com" would never match the stored "foo@bar.com". An unknown email
// and a wrong password both surface as ErrInvalidCredentials so the response
// never reveals whether an email is registered.
func (s *AuthService) Login(ctx context.Context, email, password string) (*model.User, error) {

	email = strings.ToLower(strings.TrimSpace(email))
	_, err := mail.ParseAddress(email)
	if err != nil {
		return nil, ErrInvalidInput
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !VerifyPassword(user.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}
