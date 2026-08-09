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

// NOTE ON IMPORTS: when you fill in the bodies below, add these imports to this
// file:
//
//	"errors"
//	"fmt"
//	"mail"
//	"strings"
//	"golang.org/x/crypto/bcrypt"
//	"github.com/P-kaizoku/small-light/internal/repository"
//
// They are intentionally absent right now so the scaffold compiles with empty
// bodies.

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
//
// bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost). It never
// fails in practice but always propagate the error anyway.
func HashPassword(password string) (string, error) {
	hashPass, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return "", err
	}
	return string(hashPass), nil
}

// VerifyPassword reports whether password matches a bcrypt hash.
//
// bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateToken mints a signed HS256 JWT for the given user.
//
// Build a Claims value:
//   - UserID: the id argument
//   - RegisteredClaims: Issuer = jwtIssuer, Subject = userID.String(),
//     IssuedAt = now, ExpiresAt = now.Add(s.ttl)   (now := time.Now())
//
// then jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret).
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
//
// Use jwt.ParseWithClaims(token, &Claims{}, keyfunc) where keyfunc returns
// s.secret, plus the options:
//   - jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()})
//   - jwt.WithIssuer(jwtIssuer)
//
// Return an error if parsing fails or token.Valid is false.
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

// Register creates a user and returns it.
//
//  1. Normalize the email: strings.ToLower(strings.TrimSpace(email)).
//  2. Validate: email must pass mail.ParseAddress and len(password) >=
//     minPasswordLen; otherwise return ErrInvalidInput.
//  3. hash, err := HashPassword(password); propagate err.
//  4. user, err := s.users.Create(ctx, email, hash); propagate err.
//  5. If errors.Is(err, repository.ErrConflict), return ErrDuplicateEmail
//     instead of the raw conflict.
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
//  1. Normalize the email EXACTLY like Register does (otherwise logins for
//     "Foo@Bar.com" will never match the stored "foo@bar.com").
//  2. user, err := s.users.GetByEmail(ctx, email); propagate err.
//  3. If errors.Is(err, repository.ErrNotFound) return ErrInvalidCredentials.
//     Never reveal whether the email exists.
//  4. If !VerifyPassword(user.PasswordHash, password) return
//     ErrInvalidCredentials.
//  5. Return the user.
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
