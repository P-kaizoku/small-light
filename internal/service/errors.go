package service

import "errors"

// Domain errors. These are the ONLY errors the service layer invents itself.
// repository.ErrNotFound / repository.ErrConflict pass through untouched where
// they already express what the handler wants (e.g. a missing link is just 404).
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrDuplicateEmail     = errors.New("email already registered")
	ErrInvalidInput       = errors.New("invalid input")
	ErrInvalidURL         = errors.New("invalid url")
	ErrLinkExpired        = errors.New("link expired")
)
